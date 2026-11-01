package opencode

import (
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"reflect"
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

// OpenCode reads the AGENTS.md the Codex build writes: once a project rule
// changes and Codex has not rebuilt, OpenCode's check and doctor fail STALE,
// its build warns; a hand-written AGENTS.md is the Codex check's to report.
func TestOpenCodeFlagsAStaleAGENTS(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	agents := filepath.Join(root, "AGENTS.md")
	rule := filepath.Join(root, ".claude", "rules", "always.md")
	writeOpenCodeFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
	writeOpenCodeFixture(t, rule, "Always.\n")
	if result, err := codex.Build(codex.Options{Root: root, Home: home}); err != nil || !result.OK {
		t.Fatalf("codex build: result=%#v err=%v", result, err)
	}
	if result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild}); err != nil || !result.OK {
		t.Fatalf("opencode build: result=%#v err=%v", result, err)
	}
	for _, mode := range []Mode{ModeCheck, ModeDoctor} {
		if result, err := Compile(Options{Root: root, Home: home, Mode: mode}); err != nil || !result.OK {
			t.Fatalf("mode %v on a fresh AGENTS.md: result=%#v err=%v", mode, result, err)
		}
	}

	writeOpenCodeFixture(t, rule, "Always, edited.\n")
	stale := "STALE " + agents
	for _, mode := range []Mode{ModeCheck, ModeDoctor} {
		result, err := Compile(Options{Root: root, Home: home, Mode: mode})
		if want := []string{stale + " — OpenCode reads it; run engine-sync codex build"}; err != nil || result.OK || !reflect.DeepEqual(result.Problems, want) {
			t.Fatalf("mode %v after a rule edit: OK=%v problems=%q err=%v, want %q", mode, result.OK, result.Problems, err, want)
		}
	}
	if result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild}); err != nil || !result.OK || !reflect.DeepEqual(result.Warnings, []string{stale}) {
		t.Fatalf("build after a rule edit: OK=%v warnings=%q err=%v, want %q", result.OK, result.Warnings, err, []string{stale})
	}

	// A child whose rule does not compile is named and leaves the root's
	// STALE row standing.
	writeOpenCodeFixture(t, filepath.Join(root, "app", "CLAUDE.md"), "# App\n")
	broken := filepath.Join(root, "app", ".claude", "rules", "broken.md")
	writeOpenCodeFixture(t, broken, "---\npaths:\n  - a: [unclosed\n---\nBody.\n")
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeCheck})
	wantProblems := []string{stale + " — OpenCode reads it; run engine-sync codex build"}
	wantWarnings := []string{"AGENTS.md unchecked: it does not compile: RULE-FRONTMATTER " + broken + ": line 2 — did not find expected ',' or ']'"}
	if err != nil || result.OK || !reflect.DeepEqual(result.Problems, wantProblems) || !reflect.DeepEqual(result.Warnings, wantWarnings) {
		t.Fatalf("check beside a broken child rule: OK=%v problems=%q warnings=%q err=%v, want problems %q warnings %q", result.OK, result.Problems, result.Warnings, err, wantProblems, wantWarnings)
	}
	if err := os.Remove(broken); err != nil {
		t.Fatal(err)
	}

	// Rules that cannot be read leave their AGENTS.md unchecked, named, never
	// a STALE compiled without them, and stop no OpenCode output.
	if !testjail.RanUnprivileged(t) {
		rules := filepath.Dir(rule)
		if err := os.Chmod(rules, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(rules, 0o755) })
		result, err := Compile(Options{Root: root, Home: home, Mode: ModeCheck})
		if err := os.Chmod(rules, 0o755); err != nil {
			t.Fatal(err)
		}
		if want := []string{"AGENTS.md unchecked: it does not compile: read rules " + rules + ": open " + rules + ": permission denied"}; err != nil || !result.OK || len(result.Problems) != 0 || !reflect.DeepEqual(result.Warnings, want) {
			t.Fatalf("check over unreadable rules: OK=%v problems=%q warnings=%q err=%v, want OK with warnings %q", result.OK, result.Problems, result.Warnings, err, want)
		}
	}

	writeOpenCodeFixture(t, agents, "hand written\n")
	if result, err := Compile(Options{Root: root, Home: home, Mode: ModeCheck}); err != nil || !result.OK || len(result.Problems) != 0 {
		t.Fatalf("check over a hand-written AGENTS.md: result=%#v err=%v", result, err)
	}
}
