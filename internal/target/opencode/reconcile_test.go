package opencode

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpenCodeRebuildableMirrorProblems(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		writeOpenCodeFixture(
			t,
			filepath.Join(root, ".claude", "commands", name+".md"),
			"---\ndescription: Command.\n---\nCommand.\n",
		)
	}
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("seed=%#v err=%v", result, err)
	}
	output := filepath.Join(root, ".opencode", "command")
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "commands", "a.md"), "---\ndescription: Command.\n---\nEdited.\n")
	if err := os.Remove(filepath.Join(output, "b.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(output, "c.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeFixture(t, filepath.Join(output, "d.md"), "hand\n")
	if err := os.Remove(filepath.Join(root, ".claude", "commands", "e.md")); err != nil {
		t.Fatal(err)
	}
	result, err = Compile(Options{Root: root, Home: home, Mode: ModeCheck})
	want := []string{
		"STALE " + filepath.Join(output, "a.md"),
		"MISSING " + filepath.Join(output, "b.md"),
		"STALE " + filepath.Join(output, "c.md") + " (mode want 0644, have 0600)",
		"CONFLICT " + filepath.Join(output, "d.md") + " — exists without a generated marker; not touching it",
		"ORPHAN " + filepath.Join(output, "e.md"),
	}
	rebuildable := []string{want[0], want[1], want[2], want[4]}
	if err != nil || result.OK || !reflect.DeepEqual(result.Problems, want) ||
		!reflect.DeepEqual(result.Rebuildable, rebuildable) {
		t.Fatalf("check=%#v err=%v, want problems=%q rebuildable=%q", result, err, want, rebuildable)
	}
}
