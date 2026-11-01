package codex

import (
	"fmt"
	"path/filepath"
	"testing"
)

// The AGENTS.md chain Codex reads is held to project_doc_max_bytes: the
// child's chain counts the root's AGENTS.md too; over it is a build warning
// and a check failure, never a cut file.
func TestAGENTSChainBudget(t *testing.T) {
	rootSize, appSize := len(wantRootAGENTS), len(wantAppAGENTS)
	overRow := func(root string, limit int) string {
		app := filepath.Join(root, "app", "AGENTS.md")
		return fmt.Sprintf("AGENTS-BUDGET %s: its Codex chain is %d > %d bytes (AGENTS.md %d + app/AGENTS.md %d); Codex reads none past the limit", app, rootSize+appSize, limit, rootSize, appSize)
	}
	const remedy = " — shorten CLAUDE.md or an always-on project rule, give a rule `paths:`, or raise project_doc_max_bytes in .codex/config.toml"
	for _, tc := range []struct {
		name  string
		limit int
		over  bool
	}{
		{"one byte under the chain", rootSize + appSize - 1, true},
		{"at the chain", rootSize + appSize, false},
		{"one byte over the chain", rootSize + appSize + 1, false},
		{"zero turns project docs off", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, home := projectRulesFixture(t)
			writeCodexFixture(t, filepath.Join(root, ".codex", "config.toml"), fmt.Sprintf("project_doc_max_bytes = %d\n", tc.limit))
			built, err := Build(Options{Root: root, Home: home})
			if err != nil || !built.OK {
				t.Fatalf("build: result=%#v err=%v", built, err)
			}
			if got := string(readCodexFixture(t, filepath.Join(root, "app", "AGENTS.md"))); got != wantAppAGENTS {
				t.Fatalf("app/AGENTS.md was not written whole:\n%s", got)
			}
			checked, err := CheckProject(Options{Root: root, Home: home})
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			var wantWarnings, wantProblems []string
			if tc.over {
				wantWarnings, wantProblems = []string{overRow(root, tc.limit)}, []string{overRow(root, tc.limit) + remedy}
			}
			if !sameRows(built.Warnings, wantWarnings) {
				t.Fatalf("build warnings = %q, want %q", built.Warnings, wantWarnings)
			}
			if checked.OK != !tc.over || !sameRows(checked.Problems, wantProblems) {
				t.Fatalf("check: OK=%v problems=%q, want %q", checked.OK, checked.Problems, wantProblems)
			}
		})
	}

	// With no project limit, Codex applies its home's: the budget follows it.
	t.Run("the Codex home's limit applies when the project sets none", func(t *testing.T) {
		root, home := projectRulesFixture(t)
		limit := rootSize + appSize - 1
		writeCodexFixture(t, filepath.Join(home, ".codex", "config.toml"), fmt.Sprintf("project_doc_max_bytes = %d\n", limit))
		if built, err := Build(Options{Root: root, Home: home}); err != nil || !built.OK {
			t.Fatalf("build: result=%#v err=%v", built, err)
		}
		checked, err := CheckProject(Options{Root: root, Home: home})
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if want := []string{overRow(root, limit) + remedy}; checked.OK || !sameRows(checked.Problems, want) {
			t.Fatalf("check: OK=%v problems=%q, want %q", checked.OK, checked.Problems, want)
		}
	})

	for _, tc := range []struct{ name, value, want string }{
		{"an unparseable limit is an error", "\"big\"", "AGENTS-BUDGET read project_doc_max_bytes from %s: "},
		{"a limit below zero is an error", "-1", "AGENTS-BUDGET project_doc_max_bytes in %s is -1: it must be 0 or more"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, home := projectRulesFixture(t)
			config := filepath.Join(root, ".codex", "config.toml")
			writeCodexFixture(t, config, "project_doc_max_bytes = "+tc.value+"\n")
			result, err := Build(Options{Root: root, Home: home})
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			prefix := fmt.Sprintf(tc.want, config)
			if result.OK || !containsPrefix(result.Problems, prefix) {
				t.Fatalf("build: OK=%v problems=%q, want one starting %q", result.OK, result.Problems, prefix)
			}
		})
	}
}
