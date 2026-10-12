package machine

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/testjail"
)

// TestFreeDestinationNeverReusesAnAdoptedPath: an adoption whose destination
// exists, or was taken earlier in the same run, gets the next numeric suffix.
func TestFreeDestinationNeverReusesAnAdoptedPath(t *testing.T) {
	dir := testjail.Scratch(t)
	to := filepath.Join(dir, "lead.md")
	plantFile(t, to, []byte("earlier\n"))
	for _, tc := range []struct {
		name  string
		taken map[string]bool
		want  string
	}{
		{name: "existing file", want: filepath.Join(dir, "lead.1.md")},
		{name: "existing and taken", taken: map[string]bool{filepath.Join(dir, "lead.1.md"): true}, want: filepath.Join(dir, "lead.2.md")},
		{name: "free", want: filepath.Join(dir, "other.md")},
	} {
		in := to
		if tc.name == "free" {
			in = filepath.Join(dir, "other.md")
		}
		got, err := freeDestination(in, tc.taken)
		if err != nil || got != tc.want {
			t.Fatalf("%s: freeDestination = %q (%v), want %q", tc.name, got, err, tc.want)
		}
	}
}

// TestAdoptOneRechecksOwnershipAndKeepsEarlierCopies: right before the move
// a file that now carries engine-sync's marker, or is no longer a regular
// file, is left in place and named; a destination that appeared since the
// plan gets a suffix, and the earlier adopted copy stays untouched.
func TestAdoptOneRechecksOwnershipAndKeepsEarlierCopies(t *testing.T) {
	root := testjail.Scratch(t)
	adopted := filepath.Join(root, "adopted")
	owned := filepath.Join(root, "agents", "owned.md")
	linked := filepath.Join(root, "agents", "linked.md")
	mine := filepath.Join(root, "agents", "lead.md")
	plantDir(t, filepath.Join(root, "agents"))
	plantDir(t, adopted)
	plantFile(t, owned, []byte(artifact.Marker(artifact.YAMLFront, "agents/owned.md")+"\nname: owned\n"))
	plantLink(t, linked, []byte("mine\n"))
	plantFile(t, mine, []byte("mine\n"))
	plantFile(t, filepath.Join(adopted, "lead.md"), []byte("earlier\n"))

	r := &run{}
	for _, a := range []adoption{
		{from: owned, to: filepath.Join(adopted, "owned.md")},
		{from: linked, to: filepath.Join(adopted, "linked.md")},
		{from: mine, to: filepath.Join(adopted, "lead.md")},
	} {
		if err := r.adoptOne(a); err != nil {
			t.Fatalf("adoptOne(%s) = %v", a.from, err)
		}
	}
	for path, want := range map[string]string{
		owned:                               "name: owned",
		filepath.Join(adopted, "lead.md"):   "earlier\n",
		filepath.Join(adopted, "lead.1.md"): "mine\n",
	} {
		if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), want) {
			t.Fatalf("%s = %q (%v), want %q", path, raw, err, want)
		}
	}
	for _, gone := range []string{mine, filepath.Join(adopted, "owned.md"), filepath.Join(adopted, "linked.md")} {
		if _, err := os.Lstat(gone); err == nil {
			t.Fatalf("%s present, want it absent", gone)
		}
	}
	var text []string
	for _, e := range r.report.Events {
		text = append(text, e.Message)
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{owned + " not adopted: it carries the engine-sync marker", linked + " not adopted: no longer a regular file", "adopted " + mine + " -> " + filepath.Join(adopted, "lead.1.md")} {
		if !strings.Contains(all, want) {
			t.Fatalf("events lack %q: %s", want, all)
		}
	}
}

// TestAdoptOneSkipsAFileItCannotMove: a move refused (EPERM under Linux
// protected_hardlinks, a filesystem without hard links) leaves the file in
// place and named, never aborting the run, and a destination another writer
// created since the plan (EEXIST) takes the next free suffix.
func TestAdoptOneSkipsAFileItCannotMove(t *testing.T) {
	root := testjail.Scratch(t)
	adopted := filepath.Join(root, "adopted")
	refused := filepath.Join(root, "agents", "refused.md")
	raced := filepath.Join(root, "agents", "raced.md")
	plantDir(t, filepath.Join(root, "agents"))
	plantDir(t, adopted)
	plantFile(t, refused, []byte("mine\n"))
	plantFile(t, raced, []byte("mine too\n"))
	realMove := moveFile
	t.Cleanup(func() { moveFile = realMove })
	moveFile = func(from, to string) error {
		if from == refused {
			return &os.LinkError{Op: "link", Old: from, New: to, Err: syscall.EPERM}
		}
		if from == raced && filepath.Base(to) == "raced.md" {
			plantFile(t, to, []byte("racer\n"))
		}
		return realMove(from, to)
	}
	r := &run{}
	for _, a := range []adoption{
		{from: refused, to: filepath.Join(adopted, "refused.md")},
		{from: raced, to: filepath.Join(adopted, "raced.md")},
	} {
		if err := r.adoptOne(a); err != nil {
			t.Fatalf("adoptOne(%s) = %v, want the refusal named and the run going on", a.from, err)
		}
	}
	for path, want := range map[string]string{
		refused:                              "mine\n",
		filepath.Join(adopted, "raced.md"):   "racer\n",
		filepath.Join(adopted, "raced.1.md"): "mine too\n",
	} {
		if raw, err := os.ReadFile(path); err != nil || string(raw) != want {
			t.Fatalf("%s = %q (%v), want %q", path, raw, err, want)
		}
	}
	var text []string
	for _, e := range r.report.Events {
		text = append(text, e.Message)
	}
	if all := strings.Join(text, "\n"); !strings.Contains(all, refused+" not adopted: ") || !strings.Contains(all, "adopted "+raced+" -> "+filepath.Join(adopted, "raced.1.md")) {
		t.Fatalf("events = %s, want the refused file named and the raced one moved to the next suffix", all)
	}
}
