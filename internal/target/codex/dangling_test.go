package codex

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

const dangleRemedy = "retire the stale link; `engine-sync global` prunes an orphaned global-command link automatically"

// TestDanglingGlobalCommandViaRunGlobalCommandsGatesCheckNotBuildWithRemedy
// pins the SECOND ModeCheck promotion site: RunGlobalCommands, the installer
// entry point that compiles $HOME/.claude/commands alone (no repository
// root). It must show the identical asymmetry and remedy as the repository
// path above, since the fix touched both sites independently.
func TestDanglingGlobalCommandViaRunGlobalCommandsGatesCheckNotBuildWithRemedy(t *testing.T) {
	home := testjail.Scratch(t)
	commandDir := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-global-command.md", filepath.Join(commandDir, "gone-machine.md")); err != nil {
		t.Fatal(err)
	}

	check, err := RunGlobalCommands(GlobalCommandsOptions{Home: home, Mode: ModeCheck})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if check.OK {
		t.Fatalf("check.OK = true, want false for a dangling global command source: %#v", check)
	}
	if !containsFinding(check.Problems, "gone-machine.md") {
		t.Fatalf("check problems do not name the dangling path: %#v", check.Problems)
	}
	if !containsFinding(check.Problems, dangleRemedy) {
		t.Fatalf("check problem missing the engine-sync global remedy clause: %#v", check.Problems)
	}

	build, err := RunGlobalCommands(GlobalCommandsOptions{Home: home, Mode: ModeBuild})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !build.OK {
		t.Fatalf("build.OK = false, want true: a dangling source must not gate build: %#v", build)
	}
	if !containsFinding(build.Dangling, "gone-machine.md") {
		t.Fatalf("Result.Dangling was not populated: %#v", build.Dangling)
	}
}

// TestCyclicWarningMentioningDanglingDoesNotGateCheck is the case that
// actually pins the fix. Under the old classifier, ANY warning whose
// rendered text happened to contain "dangling" armed the Check gate — a
// coincidence detector, not a real defect signal. Here a command directory
// is merely NAMED "dangling"; walking into a same-directory symlink loop
// beneath it produces an ordinary "skip cyclic command directory ...dangling/loop"
// warning that was never recorded through danglingSource. That warning must
// stay a warning: Check must read the typed Result.Dangling field, never
// scan Warnings text.
func TestCyclicWarningMentioningDanglingDoesNotGateCheck(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
	danglingNamedDir := filepath.Join(root, ".claude", "commands", "dangling")
	if err := os.MkdirAll(danglingNamedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A symlink to "." inside "dangling" resolves back to danglingNamedDir
	// itself: walking into it hits the already-visited branch and emits a
	// cyclic-directory warning whose path contains "dangling" — with no
	// source ever missing.
	if err := os.Symlink(".", filepath.Join(danglingNamedDir, "loop")); err != nil {
		t.Fatal(err)
	}

	// Build first so the fixture is at rest: Check must fail only on the
	// cyclic-named-"dangling" warning under test, never on an unrelated
	// MISSING/STALE finding from a never-materialized AGENTS.md.
	if build, err := Build(Options{Root: root, Home: home}); err != nil || !build.OK {
		t.Fatalf("seed build: result=%#v err=%v", build, err)
	}

	check, err := CheckProject(Options{Root: root, Home: home})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !containsFinding(check.Warnings, "dangling") {
		t.Fatalf(
			"fixture did not produce a warning mentioning \"dangling\" via the cyclic path — test no longer exercises the coincidence case: %#v",
			check.Warnings,
		)
	}
	if len(check.Dangling) != 0 {
		t.Fatalf(
			"Result.Dangling must stay empty for a merely-named cyclic path, not a real dangling source: %#v",
			check.Dangling,
		)
	}
	if !check.OK {
		t.Fatalf(
			"a warning that merely CONTAINS the word \"dangling\" gated Check; classification must be typed (Result.Dangling), never a textual scan of Warnings: %#v",
			check.Problems,
		)
	}
}
