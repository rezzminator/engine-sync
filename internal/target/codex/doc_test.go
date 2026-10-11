package codex

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

// TestDanglingRepoCommandGatesCheckNotBuildWithRemedy pins cases 1-3 against
// the repository-command source tree, which Run's own ModeCheck promotion
// block (compiler.go, first site) reads from result.Dangling.
func TestDanglingRepoCommandGatesCheckNotBuildWithRemedy(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
	commandDir := filepath.Join(root, ".claude", "commands")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-command.md", filepath.Join(commandDir, "gone.md")); err != nil {
		t.Fatal(err)
	}

	// Case 1: Check gates on the dangling source and names the path.
	check, err := CheckProject(Options{Root: root, Home: home})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if check.OK {
		t.Fatalf("check.OK = true, want false for a dangling repo command source: %#v", check)
	}
	if !containsFinding(check.Problems, "gone.md") {
		t.Fatalf("check problems do not name the dangling path: %#v", check.Problems)
	}
	// Case 3: the promoted problem carries the operator remedy.
	if !containsFinding(check.Problems, dangleRemedy) {
		t.Fatalf("check problem missing the engine-sync global remedy clause: %#v", check.Problems)
	}

	// Case 2: the SAME fixture through Build must not gate — Build tolerates
	// a dangling source and still writes.
	build, err := Build(Options{Root: root, Home: home})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !build.OK {
		t.Fatalf("build.OK = false, want true: a dangling source must not gate build: %#v", build)
	}
	if !containsFinding(build.Warnings, "dangling") {
		t.Fatalf("build did not warn about the dangling source: %#v", build.Warnings)
	}
	if !containsFinding(build.Dangling, "gone.md") {
		t.Fatalf("Result.Dangling was not populated for the discoverMarkdown symlink-stat emitter: %#v", build.Dangling)
	}
	if containsFinding(build.Problems, "gone.md") {
		t.Fatalf("build gated on the dangling source, breaking the build/check asymmetry: %#v", build.Problems)
	}
}
