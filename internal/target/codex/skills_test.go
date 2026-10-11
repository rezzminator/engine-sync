package codex

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

// TestDanglingRepoSkillLinkGatesCheckAndPopulatesDangling reaches the
// compileRepoSkills emitter (the fourth danglingSource call site): a broken
// symlink directly under .claude/skills, which os.ReadDir lists without
// following, so the dangling condition surfaces only on the subsequent
// os.Stat.
func TestDanglingRepoSkillLinkGatesCheckAndPopulatesDangling(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
	skillsDir := filepath.Join(root, ".claude", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(root, "does-not-exist-skill-target"),
		filepath.Join(skillsDir, "broken-skill"),
	); err != nil {
		t.Fatal(err)
	}

	check, err := CheckProject(Options{Root: root, Home: home})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if check.OK {
		t.Fatalf("check.OK = true, want false for a dangling skill link: %#v", check)
	}
	if !containsFinding(check.Problems, "broken-skill") {
		t.Fatalf("check problems do not name the dangling skill link: %#v", check.Problems)
	}
	if !containsFinding(check.Problems, dangleRemedy) {
		t.Fatalf("check problem missing the engine-sync global remedy clause: %#v", check.Problems)
	}

	build, err := Build(Options{Root: root, Home: home})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !build.OK {
		t.Fatalf("build.OK = false, want true: a dangling skill link must not gate build: %#v", build)
	}
	if !containsFinding(build.Dangling, "broken-skill") {
		t.Fatalf("Result.Dangling was not populated for the compileRepoSkills emitter: %#v", build.Dangling)
	}
}
