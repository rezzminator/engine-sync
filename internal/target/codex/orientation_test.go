package codex

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestGlobalCommandsOnlyReconcilePreservesForeignFilesAndDeletesManagedOrphans(t *testing.T) {
	home := testjail.Scratch(t)
	source := filepath.Join(home, ".claude", "commands", "fixture.md")
	writeCodexFixture(t, source, "---\ndescription: fixture\n---\nUse /fixture.\n")
	build, err := RunGlobalCommands(GlobalCommandsOptions{Home: home, Mode: ModeBuild})
	if err != nil || !build.OK || build.Wrote != 2 {
		t.Fatalf("initial global build: result=%#v err=%v", build, err)
	}
	prompt := filepath.Join(home, ".codex", "prompts", "fixture.md")
	skill := filepath.Join(home, ".codex", "skills", "fixture", "SKILL.md")
	for _, path := range []string{prompt, skill} {
		if !generatedBytes(readCodexFixture(t, path)) {
			t.Fatalf("generated output %s has no ownership marker", path)
		}
	}

	foreignPrompt := filepath.Join(home, ".codex", "prompts", "foreign.md")
	foreignSkill := filepath.Join(home, ".codex", "skills", "foreign", "SKILL.md")
	writeCodexFixture(t, foreignPrompt, "operator prompt\n")
	writeCodexFixture(t, foreignSkill, "operator skill\n")
	foreignLink := filepath.Join(home, ".codex", "skills", "foreign-link")
	if err := os.Symlink(filepath.Join(home, "operator-skill"), foreignLink); err != nil {
		t.Fatal(err)
	}
	managedOrphanPrompt := filepath.Join(home, ".codex", "prompts", "swap.md")
	managedOrphanSkill := filepath.Join(home, ".codex", "skills", "swap", "SKILL.md")
	writeCodexFixture(t, managedOrphanPrompt, generatedHeader("swap")+"\n")
	writeCodexFixture(t, managedOrphanSkill, generatedHeader("swap")+"\n")
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}

	check, err := RunGlobalCommands(GlobalCommandsOptions{Home: home, Mode: ModeCheck})
	if err != nil || check.OK || !containsFinding(check.Problems, "ORPHAN") {
		t.Fatalf("read-only global check: result=%#v err=%v", check, err)
	}
	for _, path := range []string{prompt, skill, managedOrphanPrompt, managedOrphanSkill, foreignPrompt, foreignSkill, foreignLink} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("check mutated %s: %v", path, err)
		}
	}

	build, err = RunGlobalCommands(GlobalCommandsOptions{Home: home, Mode: ModeBuild})
	if err != nil || !build.OK || build.Deleted != 4 {
		t.Fatalf("orphan cleanup: result=%#v err=%v", build, err)
	}
	for _, path := range []string{prompt, skill, managedOrphanPrompt, managedOrphanSkill} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("managed orphan remains at %s: %v", path, err)
		}
	}
	for _, path := range []string{foreignPrompt, foreignSkill, foreignLink} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("foreign file was removed at %s: %v", path, err)
		}
	}
}
