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
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

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

// NewerWriterError names the newer version and who wrote it.
type NewerWriterError struct {
	Record Record
	Mine   string
}

func (e *NewerWriterError) Error() string {
	where := "the engine-sync plugin of the Claude config dir that runs " + e.Record.Writer
	if config := os.Getenv("CLAUDE_CONFIG_DIR"); config != "" {
		where = "the engine-sync plugin in " + config
	}
	return fmt.Sprintf("NEWER-WRITER %s — %s was last written by engine-sync %s (%s), this is %s; update %s",
		e.Record.Version, e.Record.Root, e.Record.Version, e.Record.Writer, e.Mine, where)
}

func (e *NewerWriterError) Unwrap() error { return ErrNewerWriter }

// key is the file stem every root's lock and record share.
func key(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return hex.EncodeToString(sum[:])[:16]
}

// Lock takes the exclusive per-root lock under state and returns its release.
// It blocks until a concurrent writer of the same root is done.
func Lock(state, root string) (func() error, error) {
	dir := filepath.Join(state, "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory %s: %w", dir, err)
	}
	path := filepath.Join(dir, key(root)+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return nil, errors.Join(fmt.Errorf("lock %s: %w", path, err), file.Close())
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
	path := filepath.Join(state, "writers", key(root)+".json")
	raw, err := os.ReadFile(path)
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
	dir := filepath.Join(state, "writers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create writer records %s: %w", dir, err)
	}
	path := filepath.Join(dir, key(root)+".json")
	if raw, err := os.ReadFile(path); err == nil {
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
	coreA, preA, okA := parse(a)
	coreB, preB, okB := parse(b)
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

func parse(version string) ([3]int, string, bool) {
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
