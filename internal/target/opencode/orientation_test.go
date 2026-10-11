package opencode

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"testing"
)

func TestOpenCodeMirrorCopyConflictNamesTheRemedy(t *testing.T) {
	root := testjail.Scratch(t)
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"opencode":{"modelMap":{"sonnet":"openai/gpt-6.1-sol-fast"},"mirrorFiles":["LICENSE"]}}`)
	home := filepath.Join(root, "home")
	writeOpenCodeFixture(t, filepath.Join(root, "LICENSE"), "the source license\n")
	copyPath := filepath.Join(root, ".opencode", "LICENSE")
	writeOpenCodeFixture(t, copyPath, "a different license\n")

	result, err := Compile(Options{Root: root, Home: home, Mode: ModeCheck})
	want := "CONFLICT " + copyPath + " — differs from " + filepath.Join(root, "LICENSE") +
		" and engine-sync cannot prove it wrote it; preserved; move it aside before rebuilding"
	if err != nil || result.OK || !containsProblem(result.Problems, want) {
		t.Fatalf("problems=%q err=%v, want %q", result.Problems, err, want)
	}
}
