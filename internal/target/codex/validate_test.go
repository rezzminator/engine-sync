package codex

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectAgentCodexRoleSettings(t *testing.T) {
	for _, tc := range []struct {
		name, file, frontmatter string
		refused                 bool
	}{
		{name: "project tier honours the keys", file: "lab.md", frontmatter: "model: opus\ncodex-model: gpt-fixture-lab\ncodex-effort: low\ncodex-sandbox: workspace-write\ntools: Read, Grep\n"},
		{name: "project tier refusal", file: "orch.md", frontmatter: "model: claude-sonnet-5-5\n", refused: true},
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
			twin := filepath.Join(root, ".codex", "agents", "lab.toml")
			assertCodexContains(t, twin,
				"# tier: gpt-fixture-lab (Claude alias: opus)\n",
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
