package opencode

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSymlinkedGlobalCommandsAndCycle(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	commands := filepath.Join(home, ".claude", "commands")
	source := filepath.Join(root, "sources", "tools")
	writeOpenCodeFixture(
		t,
		filepath.Join(source, "refine.md"),
		"---\ndescription: Refine.\n---\nRead then execute /tools:review.\n",
	)
	writeOpenCodeFixture(t, filepath.Join(source, "review.md"), "---\ndescription: Review.\n---\nCheck.\n")
	if err := os.MkdirAll(commands, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(commands, "tools")); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeFixture(t, filepath.Join(commands, "context-meter", "SKILL.md"), "---\ndescription: Audit.\n---\nCount.\n")

	built, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !built.OK {
		t.Fatalf("symlinked command build result=%#v err=%v", built, err)
	}
	for _, path := range []string{
		filepath.Join(home, ".config", openCodeName(), "command", "tools-refine.md"),
		filepath.Join(home, ".config", openCodeName(), "command", "tools-review.md"),
		filepath.Join(home, ".config", openCodeName(), "command", "context-meter.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing global projection %s: %v", path, err)
		}
	}
	refine, _ := os.ReadFile(filepath.Join(home, ".config", openCodeName(), "command", "tools-refine.md"))
	if !strings.Contains(string(refine), "/tools-review") {
		t.Fatalf("global symlinked command was not flattened: %s", refine)
	}
	doctor, err := Compile(Options{Root: root, Home: home, Mode: ModeDoctor})
	if err != nil || !doctor.OK {
		t.Fatalf("doctor result=%#v err=%v", doctor, err)
	}

	if err := os.Symlink(
		filepath.Join("..", "..", "home", ".claude", "commands"),
		filepath.Join(source, "cycle"),
	); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Mode{ModeBuild, ModeCheck, ModeDoctor} {
		cyclic, err := Compile(Options{Root: root, Home: home, Mode: mode})
		if err != nil || cyclic.OK || !containsProblem(cyclic.Problems, "symlink cycle") {
			t.Fatalf("mode %d: cyclic global source was not named as a failure: result=%#v err=%v", mode, cyclic, err)
		}
	}
}

func TestReviewDanglingSkillKeepsItsMirror(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	skill := filepath.Join(root, ".claude", "skills", "sample")
	writeOpenCodeFixture(t, filepath.Join(skill, "SKILL.md"), "---\ndescription: Sample.\n---\nSample.\n")
	if result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild}); err != nil || !result.OK {
		t.Fatalf("seed=%#v %v", result, err)
	}
	if err := os.RemoveAll(skill); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../missing-skill", skill); err != nil {
		t.Fatal(err)
	}
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if _, readErr := os.Lstat(filepath.Join(root, ".opencode", "skills", "sample")); readErr != nil || err != nil || !result.OK {
		t.Fatalf("dangling source erased mirror: result=%#v error=%v lstat=%v", result, err, readErr)
	}
}
