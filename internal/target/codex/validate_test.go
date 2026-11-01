package codex

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestProjectAgentCodexRoleSettings(t *testing.T) {
	for _, tc := range []struct {
		name, file, frontmatter string
		refused                 bool
		// warnings are exact, {source} standing for the agent's path; a
		// skipped agent has no role.
		warnings []string
		// problems are exact like warnings: the build fails, no role is written.
		problems []string
		role     string
	}{
		{name: "project tier honours the keys", file: "lab.md", frontmatter: "model: opus\nengines:\n  codex:\n    model: gpt-fixture-lab\n    effort: low\n    sandbox: workspace-write\ntools: Read, Grep\n"},
		{name: "project tier refusal", file: "orch.md", frontmatter: "model: claude-sonnet-5-5\n", refused: true},
		{name: "effort outside the table is omitted", file: "turbo.md", frontmatter: "effort: turbo\n",
			warnings: []string{`{source}: invalid effort "turbo" (want low, medium, high, xhigh, max) — effort omitted`},
			role:     "name = \"turbo\"\ndescription = \"Lab.\"\ndeveloper_instructions = "},
		{name: "requires that is not a list fails the agent", file: "gated.md", frontmatter: "requires: example-cap\n",
			problems: []string{"{source}: requires must be a list of capability names"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, home := testjail.Scratch(t), testjail.Scratch(t)
			writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "Root.\n")
			source := filepath.Join(root, ".claude", "agents", tc.file)
			writeCodexFixture(t, source, "---\ndescription: Lab.\n"+tc.frontmatter+"---\nLab.\n")
			result, err := Build(Options{Root: root, Home: home})
			if err != nil {
				t.Fatal(err)
			}
			twin := filepath.Join(root, ".codex", "agents", strings.TrimSuffix(tc.file, ".md")+".toml")
			if tc.problems != nil {
				want := make([]string, len(tc.problems))
				for i, p := range tc.problems {
					want[i] = strings.ReplaceAll(p, "{source}", source)
				}
				if result.OK || !slices.Equal(result.Problems, want) {
					t.Fatalf("build OK=%v problems %q, want failure with %q", result.OK, result.Problems, want)
				}
				if _, err := os.Lstat(twin); !os.IsNotExist(err) {
					t.Fatalf("failed agent has a role %s: %v", twin, err)
				}
				return
			}
			if tc.warnings != nil {
				want := make([]string, len(tc.warnings))
				for i, w := range tc.warnings {
					want[i] = strings.ReplaceAll(w, "{source}", source)
				}
				if !result.OK || !slices.Equal(result.Warnings, want) {
					t.Fatalf("build OK=%v warnings %q problems %q, want warnings %q", result.OK, result.Warnings, result.Problems, want)
				}
				assertCodexContains(t, twin, tc.role)
				return
			}
			if tc.refused {
				if !result.OK || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], source) {
					t.Fatalf("unmapped alias=%#v", result)
				}
				content := string(readCodexFixture(t, filepath.Join(root, ".codex", "agents", "orch.toml")))
				if strings.Contains(content, "\nmodel =") {
					t.Fatalf("unmapped role pinned: %s", content)
				}
				return
			}

			if !result.OK {
				t.Fatalf("build = %#v", result)
			}
			assertCodexContains(t, twin,
				"# model: gpt-fixture-lab (Claude alias: opus)\n",
				"model = \"gpt-fixture-lab\"\nmodel_reasoning_effort = \"low\"\nsandbox_mode = \"workspace-write\"\n")
			content := string(readCodexFixture(t, twin))
			if strings.Contains(content, "read-only") {
				t.Fatalf("workspace-write role is read-only:\n%s", content)
			}
			if err := validateTOML(content); err != nil {
				t.Fatal(err)
			}
		})
	}
}
