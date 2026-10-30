// Package guard keeps two writers of one output apart. A per-root lock
// serialises concurrent writes, and a writer record keeps an older binary
// from overwriting a newer one's output: every write records the writer's
// version in {engine-sync state}/writers/, outside every output, so an
// upgrade churns no generated file, and a binary older than the record
// writes nothing and reports NEWER-WRITER.
package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"golang.org/x/text/unicode/norm"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rezzminator/engine-sync/internal/atomicfile"
)

// Record is what the writer of one output root left behind.
type Record struct {
	Version string `json:"version"`
	Root    string `json:"root"`
	Writer  string `json:"writer"`
}

// ErrNewerWriter reports an output a newer binary wrote.
var ErrNewerWriter = errors.New("NEWER-WRITER")

// ErrBusy reports a lock a live process holds past the wait: flock frees a
// dead holder's lock, so a busy lock always has a live holder.
var ErrBusy = errors.New("BUSY")

// BusyCode is the exit code of a run refused by ErrBusy, the in-process and
// cross-process channel a caller tells it apart by (EX_TEMPFAIL).
const BusyCode = 75

// NewerWriterError names the newer version and who wrote it.
type NewerWriterError struct {
	Record Record
	Mine   string
}

func (e *NewerWriterError) Error() string {
	where := "the engine-sync plugin of the Claude config dir that runs " + e.Record.Writer
	if config := paths.Getenv("CLAUDE_CONFIG_DIR"); config != "" {
		where = "the engine-sync plugin in " + config
	}
	return fmt.Sprintf("NEWER-WRITER %s — %s was last written by engine-sync %s (%s), this is %s; update %s",
		e.Record.Version, e.Record.Root, e.Record.Version, e.Record.Writer, e.Mine, where)
}

func (e *NewerWriterError) Unwrap() error { return ErrNewerWriter }

// key is the file stem every root's lock and record share.
func key(root string) string {
	sum := sha256.Sum256([]byte(norm.NFC.String(filepath.Clean(root))))
	return hex.EncodeToString(sum[:])[:16]
}

// Lock takes the exclusive per-root lock under state and returns its release.
// It blocks until a concurrent writer of the same root is done.
func Lock(state, root string, wait ...time.Duration) (func() error, error) {
	var e error
	root, e = CanonicalRoot(root)
	if e != nil {
		return nil, e
	}
	dir := filepath.Join(state, "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory %s: %w", dir, err)
	}
	path := filepath.Join(dir, key(root)+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	deadline := time.Time{}
	if len(wait) > 0 && wait[0] > 0 {
		deadline = time.Now().Add(wait[0])
	}
	for {
		mode := syscall.LOCK_EX
		if !deadline.IsZero() {
			mode |= syscall.LOCK_NB
		}
		err := syscall.Flock(int(file.Fd()), mode)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, errors.Join(fmt.Errorf("lock %s: %w", path, err), file.Close())
		}
		if time.Now().After(deadline) {
			owner := make([]byte, 1024)
			n, readErr := file.ReadAt(owner, 0)
			if readErr != nil && n == 0 {
				return nil, errors.Join(fmt.Errorf("%w lock %s: holder unavailable: %w", ErrBusy, path, readErr), file.Close())
			}
			return nil, errors.Join(fmt.Errorf("%w lock %s: holder %s; deadline exceeded", ErrBusy, path, strings.TrimSpace(string(owner[:n]))), file.Close())
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := file.Truncate(0); err != nil {
		return nil, errors.Join(fmt.Errorf("truncate lock owner %s: %w", path, err), file.Close())
	}
	if _, err := fmt.Fprintf(file, "pid=%d root=%s\n", os.Getpid(), root); err != nil {
		return nil, errors.Join(fmt.Errorf("write lock owner %s: %w", path, err), file.Close())
	}
	return func() error {
		unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		if err := errors.Join(unlockErr, file.Close()); err != nil {
			return fmt.Errorf("release lock %s: %w", path, err)
		}
		return nil
	}, nil
}

// Check refuses a write by version mine when a newer version wrote root. No
// record, or an unparseable version on either side, admits the write: only a
// proven newer writer refuses. An unreadable record is an error, never a pass.
func Check(state, root, mine string) error {
	var e error
	root, e = CanonicalRoot(root)
	if e != nil {
		return e
	}
	path := filepath.Join(state, "writers", key(root)+".json")
	raw, err := paths.ReadRegular(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read writer record %s: %w", path, err)
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return fmt.Errorf("parse writer record %s: %w", path, err)
	}
	if Newer(record.Version, mine) {
		return &NewerWriterError{Record: record, Mine: mine}
	}
	return nil
}

// Save records mine as root's writer, unless the record already names a
// version at least as new: the record moves only forward.
func Save(state, root, mine, writer string) error {
	var e error
	root, e = CanonicalRoot(root)
	if e != nil {
		return e
	}
	dir := filepath.Join(state, "writers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create writer records %s: %w", dir, err)
	}
	path := filepath.Join(dir, key(root)+".json")
	if raw, err := paths.ReadRegular(path); err == nil {
		var record Record
		if json.Unmarshal(raw, &record) == nil &&
			(Newer(record.Version, mine) || record.Version == mine && record.Writer == writer) {
			return nil
		}
	}
	raw, err := json.Marshal(Record{Version: mine, Root: filepath.Clean(root), Writer: writer})
	if err != nil {
		return fmt.Errorf("encode writer record: %w", err)
	}
	if err := atomicfile.Write(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write writer record %s: %w", path, err)
	}
	return nil
}

// Newer reports whether version a is a strictly newer release than b. Both
// must be N.N.N with an optional -suffix; a suffixed version (a pre-release)
// is older than the same core without one. Anything else compares as not
// newer.
func Newer(a, b string) bool {
	coreA, preA, okA := parseWriterVersion(a)
	coreB, preB, okB := parseWriterVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range coreA {
		if coreA[i] != coreB[i] {
			return coreA[i] > coreB[i]
		}
	}
	switch {
	case preA == preB:
		return false
	case preA == "":
		return true
	case preB == "":
		return false
	default:
		return newerPreRelease(preA, preB)
	}
}

// newerPreRelease orders two pre-release suffixes as semver does: identifier
// by identifier, numeric ones by value and below alphanumeric ones, a longer
// list above its own prefix (alpha.10 above alpha.9, alpha.1 above alpha).
func newerPreRelease(a, b string) bool {
	idsA, idsB := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(idsA) && i < len(idsB); i++ {
		if idsA[i] == idsB[i] {
			continue
		}
		numA, errA := strconv.Atoi(idsA[i])
		numB, errB := strconv.Atoi(idsB[i])
		switch {
		case errA == nil && errB == nil:
			return numA > numB
		case errA == nil:
			return false
		case errB == nil:
			return true
		default:
			return idsA[i] > idsB[i]
		}
	}
	return len(idsA) > len(idsB)
}

func parseWriterVersion(version string) ([3]int, string, bool) {
	var core [3]int
	main, pre, _ := strings.Cut(strings.TrimPrefix(version, "v"), "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return core, "", false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return core, "", false
		}
		core[i] = n
	}
	return core, pre, true
}

func CanonicalRoot(root string) (string, error) {
	prefix := ""
	// Namespaces identify independent locks, not filesystem path components.
	for _, namespace := range []string{"global:", "dirty:"} {
		if strings.HasPrefix(root, namespace) {
			prefix = namespace
			root = strings.TrimPrefix(root, prefix)
			break
		}
	}
	p, e := paths.PhysicalPath(root)
	if e != nil {
		return "", e
	}
	return prefix + p, nil
}
