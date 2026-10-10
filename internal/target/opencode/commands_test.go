package opencode

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeDanglingCommandKeepsItsTwin(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	source := filepath.Join(root, ".claude", "commands", "lab.md")
	twin := filepath.Join(root, ".opencode", "command", "lab.md")
	writeOpenCodeFixture(t, source, "---\ndescription: Lab.\n---\nLab.\n")
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("seed=%#v err=%v", result, err)
	}
	seeded, err := os.ReadFile(twin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	target := "../../vendor/lab.md"
	if err := os.Symlink(target, source); err != nil {
		t.Fatal(err)
	}
	result, err = Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	body, readErr := os.ReadFile(twin)
	warning := "source unresolvable: " + source + " → " + target + "; twin kept"
	if err != nil || !result.OK || result.Deleted != 0 || readErr != nil || !bytes.Equal(body, seeded) ||
		!containsProblem(result.Warnings, warning) {
		t.Fatalf("build=%#v err=%v body=%q read=%v, want kept twin warning %q", result, err, body, readErr, warning)
	}
	result, err = Compile(Options{Root: root, Home: home, Mode: ModeCheck})
	if err != nil || result.OK || !containsProblem(result.Problems, "DANGLING "+source) {
		t.Fatalf("check=%#v err=%v", result, err)
	}
}
