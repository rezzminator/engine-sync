package codex

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"testing"
)

func TestFullCheckAgreesWithInstallerGlobalReconciliation(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
	writeCodexFixture(t, filepath.Join(root, ".claude", "commands", "dev.md"), "---\ndescription: dev\n---\nRun /dev.\n")
	writeCodexFixture(
		t,
		filepath.Join(home, ".claude", "commands", "chat", "inject.md"),
		"---\ndescription: inject\n---\nInject.\n",
	)
	writeCodexFixture(
		t,
		filepath.Join(home, ".claude", "commands", "chat", "new.md"),
		"---\ndescription: new\n---\nContinue with /chat:inject.\n",
	)

	initial, err := Build(Options{Root: root, Home: home})
	if err != nil || !initial.OK {
		t.Fatalf("initial full build: result=%#v err=%v", initial, err)
	}
	installed, err := RunGlobalCommands(GlobalCommandsOptions{Home: home, Mode: ModeBuild})
	if err != nil || !installed.OK {
		t.Fatalf("installer global reconciliation: result=%#v err=%v", installed, err)
	}
	check, err := CheckProject(Options{Root: root, Home: home})
	if err != nil {
		t.Fatalf("full check: %v", err)
	}
	if !check.OK {
		t.Fatalf("full check disagrees with installer reconciliation: %#v", check.Problems)
	}
}
