package cli

import (
	"bytes"
	"errors"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedOpenCodeLabber builds root with a regular .claude/agents/labber.md and
// returns the source path, its twin and the twin's seeded bytes.
func seedOpenCodeLabber(t *testing.T, root, home string) (string, string, []byte) {
	t.Helper()
	source := filepath.Join(root, ".claude", "agents", "labber.md")
	twin := filepath.Join(root, ".opencode", "agent", "labber.md")
	cliWriteTestFile(t, source, "---\ndescription: Fixture lab role.\n---\nRun the lab.\n")
	if code, stdout, stderr := buildOpenCodeForTest(t, "build", root, home); code != 0 {
		t.Fatalf("seed build: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	seeded, err := os.ReadFile(twin)
	if err != nil {
		t.Fatalf("seed build wrote no twin %s: %v", twin, err)
	}
	return source, twin, seeded
}

func buildOpenCodeForTest(t *testing.T, action, root, home string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := RunOpenCodeCommand([]string{action, root, "--home", home}, func() (string, error) { return root, nil }, home, &out, &err)
	return code, out.String(), err.String()
}

// An adopter's .claude/agents/labber.md symlinked into an uninitialised
// submodule names a source pfm cannot read right now, not one the adopter
// retired: build keeps its .opencode/agent twin byte for byte and says so.
func TestOpenCodeUnresolvableAgentSourceKeepsItsTwin(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	source, twin, seeded := seedOpenCodeLabber(t, root, home)
	target := "../../vendor/lab/agents/labber.md"
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, source); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := buildOpenCodeForTest(t, "build", root, home)
	if code != 0 {
		t.Fatalf("build: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	have, err := os.ReadFile(twin)
	if err != nil {
		t.Fatalf("twin %s of the unresolvable source was removed: %v (stdout %q)", twin, err, stdout)
	}
	if !bytes.Equal(have, seeded) {
		t.Fatalf("twin %s was rewritten:\nhave %q\nwant %q", twin, have, seeded)
	}
	want := "engine-sync opencode: warning: source unresolvable: " + source + " → " + target + "; twin kept"
	if !strings.Contains(stderr, want) {
		t.Fatalf("build stderr lacks %q: %q", want, stderr)
	}
	if strings.Contains(stdout, "engine-sync opencode: deleted ") || !strings.Contains(stdout, "deleted=0") {
		t.Fatalf("build deleted something with the twin kept: %q", stdout)
	}

	code, _, stderr = buildOpenCodeForTest(t, "check", root, home)
	if strings.Contains(stderr, "ORPHAN") {
		t.Fatalf("check reports the kept twin as an orphan: %q", stderr)
	}
	if code != 1 || !strings.Contains(stderr, "engine-sync opencode: DANGLING "+source) {
		t.Fatalf("check no longer gates on the dangling source: code=%d stderr=%q", code, stderr)
	}
}

// A source that is truly gone (no file, no link) still loses its twin.
func TestOpenCodeAbsentAgentSourceStillDeletesItsTwin(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	source, twin, _ := seedOpenCodeLabber(t, root, home)
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := buildOpenCodeForTest(t, "build", root, home)
	if code != 0 {
		t.Fatalf("build: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Lstat(twin); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("twin %s of an absent source survived: %v", twin, err)
	}
	if !strings.Contains(stdout, "engine-sync opencode: deleted "+twin) {
		t.Fatalf("build did not name the deleted twin: %q", stdout)
	}
	if strings.Contains(stderr, "twin kept") {
		t.Fatalf("an absent source claimed a kept twin: %q", stderr)
	}
}
