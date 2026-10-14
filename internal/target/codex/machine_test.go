package codex

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
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
		filepath.Join(home, ".claude", "commands", "chat", "call.md"),
		"---\ndescription: Delivers a message.\n---\nCall.\n",
	)
	writeCodexFixture(
		t,
		filepath.Join(home, ".claude", "commands", "chat", "new.md"),
		"---\ndescription: new\n---\nContinue with /chat:call.\n",
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

func TestReviewMultiHomeCommands(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprint(reverse), func(t *testing.T) {
			home := testjail.Scratch(t)
			source := filepath.Join(home, ".claude", "commands")
			if e := os.MkdirAll(source, 0755); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(source, "hello.md"), []byte("---\ndescription: Hello.\n---\nHi.\n"), 0600); e != nil {
				t.Fatal(e)
			}
			homes := []string{filepath.Join(home, ".codex"), filepath.Join(home, ".codex-work")}
			if reverse {
				homes[0], homes[1] = homes[1], homes[0]
			}
			r, e := RunGlobalCommands(GlobalCommandsOptions{Home: home, CodexHomes: homes})
			if e != nil || !r.OK {
				t.Errorf("run %+v %v", r, e)
			}
			for _, h := range homes {
				if _, e := os.Stat(filepath.Join(h, "prompts", "hello.md")); e != nil {
					t.Errorf("home lost %s: %v", h, e)
				}
			}
		})
	}
}
