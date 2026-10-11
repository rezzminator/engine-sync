package paths

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const MaxSourceBytes = 8 << 20

// ReadRegular follows source aliases once, then refuses special files and bounds reads.
func ReadRegular(path string) ([]byte, error) { return readBoundedRegular(path, MaxSourceBytes, true) }

func ReadHeader(path string) ([]byte, error) { return readBoundedRegular(path, 4096, false) }

func readBoundedRegular(path string, limit int64, complete bool) ([]byte, error) {
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return nil, e
	}
	info, e := os.Stat(resolved)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read %s: not a regular file", path)
	}
	fd, e := syscall.Open(resolved, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, fmt.Errorf("open %s: %w", path, e)
	}
	f := os.NewFile(uintptr(fd), resolved)
	defer f.Close()
	info, e = f.Stat()
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read %s: not a regular file", path)
	}
	if complete && info.Size() > limit {
		return nil, fmt.Errorf("read %s: exceeds %d byte limit", path, limit)
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, fmt.Errorf("read %s: %w", path, e)
	}
	if complete && int64(len(raw)) > limit {
		return nil, fmt.Errorf("read %s: exceeds %d byte limit", path, limit)
	}
	return raw, nil
}
