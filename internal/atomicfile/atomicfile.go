// Package atomicfile is the one atomic file writer: every file engine-sync replaces
// whole — config, caches, crumbs, receipts, staged assets — goes through Write,
// so a reader sees the old content or the new, never a torn write, and a
// failure leaves the old file and no scratch behind.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"syscall"
)

// removeScratch deletes an unpublished scratch file; a variable so a test can
// make the removal fail and watch the failure surface.
var removeScratch = os.Remove

// Write replaces path with content in one rename: a scratch file beside path
// (".<name>.tmp-*", so the rename never crosses a filesystem) is written, given
// mode's permission bits, synced and closed, then renamed over path. A missing
// parent directory is created 0o755; existing directory modes are preserved.
func Write(path string, content []byte, mode fs.FileMode) (err error) {
	return publish(path, mode, func(scratch *os.File) error {
		_, err := scratch.Write(content)
		return err
	})
}

// Create publishes a complete file only if path is still absent. It never
// replaces a concurrently created file or follows a destination symlink.
func Create(path string, content []byte, mode fs.FileMode) error {
	return publish(path, mode, func(scratch *os.File) error { _, err := scratch.Write(content); return err }, true)
}

// ErrTooLarge reports a WriteFrom stream that ran past its limit; the target
// is left untouched and no scratch remains.
var ErrTooLarge = errors.New("content exceeds the size limit")

// WriteFrom streams r into path with Write's guarantees, never holding the
// whole body in memory. limit > 0 caps the stream: one byte past it is
// ErrTooLarge and nothing is published. It returns the bytes published.
func WriteFrom(path string, r io.Reader, mode fs.FileMode, limit int64) (written int64, err error) {
	err = publish(path, mode, func(scratch *os.File) error {
		source := r
		if limit > 0 {
			source = io.LimitReader(r, limit+1)
		}
		n, copyErr := io.Copy(scratch, source)
		written = n
		if copyErr != nil {
			return copyErr
		}
		if limit > 0 && n > limit {
			return fmt.Errorf("%w (%d bytes)", ErrTooLarge, limit)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}

// publish is the one scratch-then-rename sequence Write and WriteFrom share:
// fill writes the scratch file, which is then synced, closed and renamed over
// path; any failure removes the scratch and leaves path as it was.
func publish(path string, mode fs.FileMode, fill func(*os.File) error, exclusive ...bool) (err error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("write %s: create directory: %w", path, err)
	}
	scratch, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: create scratch: %w", path, err)
	}
	scratchPath := scratch.Name()
	published := false
	defer func() {
		if published {
			return
		}
		if removeErr := removeScratch(scratchPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("write %s: remove scratch %s: %w", path, scratchPath, removeErr))
		}
	}()
	if err := scratch.Chmod(mode.Perm()); err != nil {
		_ = scratch.Close()
		return fmt.Errorf("write %s: set mode: %w", path, err)
	}
	if err := fill(scratch); err != nil {
		_ = scratch.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := scratch.Sync(); err != nil {
		_ = scratch.Close()
		return fmt.Errorf("write %s: sync: %w", path, err)
	}
	if err := scratch.Close(); err != nil {
		return fmt.Errorf("write %s: close scratch: %w", path, err)
	}
	if len(exclusive) > 0 && exclusive[0] {
		if err := os.Link(scratchPath, path); err != nil {
			return fmt.Errorf("create %s: publish: %w", path, err)
		}
		if err := removeScratch(scratchPath); err != nil {
			return fmt.Errorf("create %s: remove scratch: %w", path, err)
		}
		published = true
		return syncDirectory(directory)
	}
	if err := os.Rename(scratchPath, path); err != nil {
		return fmt.Errorf("write %s: replace: %w", path, err)
	}
	published = true
	return syncDirectory(directory)
}
func syncDirectory(directory string) error {
	d, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("sync directory %s: %w", directory, err)
	}
	if err := errors.Join(d.Sync(), d.Close()); err != nil {
		return fmt.Errorf("sync directory %s: %w", directory, err)
	}
	return nil
}

// WriteScratch writes a disposable file in directory and returns its path and
// cleanup. The file is never published over a durable target, so it needs no
// rename or directory sync.
func WriteScratch(directory, pattern string, content []byte) (path string, cleanup func(), err error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", nil, fmt.Errorf("create scratch directory %s: %w", directory, err)
	}
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", nil, fmt.Errorf("create scratch file: %w", err)
	}
	path = file.Name()
	cleanup = func() { _ = os.Remove(path) }
	if _, err := file.Write(content); err != nil {
		closeErr := file.Close()
		cleanup()
		return "", nil, fmt.Errorf("write scratch file: %w", errors.Join(err, closeErr))
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close scratch file: %w", err)
	}
	return path, cleanup, nil
}

func Symlink(target, path string) error {
	scratch := filepath.Join(
		filepath.Dir(path),
		fmt.Sprintf(".%s.engine-sync-link-%d-%016x", filepath.Base(path), os.Getpid(), rand.Uint64()),
	)
	if err := os.Symlink(target, scratch); err != nil {
		return err
	}
	if err := os.Rename(scratch, path); err != nil {
		if removeErr := os.Remove(scratch); removeErr != nil {
			return fmt.Errorf("%w; remove scratch link %s: %v", err, scratch, removeErr)
		}
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// link is os.Link; a variable so a test can make it cross a filesystem.
var link = os.Link

// MoveAside renames a capped file over its previous archive on the same filesystem.
func MoveAside(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("move %s aside to %s: %w", from, to, err)
	}
	return syncDirectory(filepath.Dir(to))
}

// Move moves from to to without ever replacing a file at to: a hard link
// (refused when to exists), then from removed, both directories synced. A
// link refused because the two sit on different filesystems (EXDEV) falls
// back to a copy created exclusively beside to, synced, then from removed.
func Move(from, to string) error {
	err := link(from, to)
	if errors.Is(err, syscall.EXDEV) {
		return copyAcross(from, to)
	}
	if err != nil {
		return err
	}
	if err := os.Remove(from); err != nil {
		return errors.Join(fmt.Errorf("remove %s after linking it to %s: %w", from, to, err), os.Remove(to))
	}
	err = syncDirectory(filepath.Dir(to))
	if filepath.Dir(from) != filepath.Dir(to) {
		err = errors.Join(err, syncDirectory(filepath.Dir(from)))
	}
	return err
}

// copyAcross moves from to another filesystem: to is created exclusively with
// from's permission bits, written, synced and closed, its directory synced,
// and only then from removed. A failed copy removes the partial to and leaves
// from in place.
func copyAcross(from, to string) (err error) {
	source, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("move %s across filesystems: %w", from, err)
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close %s: %w", from, closeErr))
		}
	}()
	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("inspect %s to move it: %w", from, err)
	}
	target, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("move %s across filesystems: %w", from, err)
	}
	_, err = io.Copy(target, source)
	if err == nil {
		err = target.Chmod(info.Mode().Perm())
	}
	if err == nil {
		err = target.Sync()
	}
	err = errors.Join(err, target.Close())
	if err != nil {
		return errors.Join(fmt.Errorf("copy %s to %s: %w", from, to, err), os.Remove(to))
	}
	if err := syncDirectory(filepath.Dir(to)); err != nil {
		return err
	}
	if err := os.Remove(from); err != nil {
		return fmt.Errorf("remove %s after copying it to %s: %w", from, to, err)
	}
	return syncDirectory(filepath.Dir(from))
}
