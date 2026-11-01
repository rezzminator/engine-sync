package codex

import (
	"github.com/rezzminator/engine-sync/internal/config"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNeutralRoleUsesEngineModelDefault(t *testing.T) {
	role, warning, e := codexRoleSettings(claudesource.Frontmatter{Model: "sonnet", Fields: map[string]string{"model": "sonnet"}}, config.Default().Codex.ModelMap, "role.md")
	if e != nil || role.Model != "" || !strings.Contains(warning, `unmapped Codex model alias "sonnet"`) {
		t.Fatalf("unmapped neutral role=%#v err=%v", role, e)
	}
}

// An effort no Codex level maps is omitted, never a failed role; the source
// loader names it (ValidateAgent).
func TestCodexRoleOmitsAnEffortOutsideTheTable(t *testing.T) {
	role, warning, e := codexRoleSettings(claudesource.Frontmatter{Effort: "turbo", Fields: map[string]string{"effort": "turbo"}}, config.Default().Codex.ModelMap, "role.md")
	if e != nil || role != (codexRole{}) || warning != "" {
		t.Fatalf("role=%#v warning=%q err=%v, want no effort line and no error", role, warning, e)
	}
}

// TestGlobalAgentEscapeMultilineNeutralisesTripleQuoteCollision pins the
// multi-line basic-string escape to build-codex.mjs:153 exactly: backslash
// doubled, then a literal `"""` neutralised so it can never be mistaken for
// the string's own closing delimiter.
func TestGlobalAgentEscapeMultilineNeutralisesTripleQuoteCollision(t *testing.T) {
	in := "line one\nhas a literal \"\"\" inside\nand a backslash \\ alone"
	want := "line one\nhas a literal \\\"\\\"\\\" inside\nand a backslash \\\\ alone"
	if got := globalAgentEscapeMultiline(in); got != want {
		t.Fatalf("globalAgentEscapeMultiline(%q) = %q, want %q", in, got, want)
	}
}

// TestProjectSourceThatDoesNotParseKeepsItsTwins: an agent or command whose
// header stops parsing is named FRONTMATTER-INVALID and keeps the twin it
// built before — an agent's under the name its `name:` line still declares —
// never planned for deletion as an orphan.
func TestProjectSourceThatDoesNotParseKeepsItsTwins(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "Root.\n")
	agent := filepath.Join(root, ".claude", "agents", "lab.md")
	command := filepath.Join(root, ".claude", "commands", "ship.md")
	writeCodexFixture(t, agent, "---\nname: lab-pro\ndescription: Lab.\n---\nLab.\n")
	writeCodexFixture(t, command, "---\ndescription: Ship.\n---\nShip.\n")
	policy := config.Default()
	policy.Codex.GlobalCommands = false
	if result, err := Build(Options{Root: root, Home: home, Config: &policy}); err != nil || !result.OK {
		t.Fatalf("seed build = %#v, %v", result, err)
	}
	role := filepath.Join(root, ".codex", "agents", "lab-pro.toml")
	skill := filepath.Join(root, ".codex", "skills", "ship", "SKILL.md")
	broken := "description: Lab.\nengines:\n\tcodex: false\n---\nLab.\n"
	writeCodexFixture(t, agent, "---\nname: lab-pro\n"+broken)
	writeCodexFixture(t, command, "---\n"+broken)
	result, err := CheckProject(Options{Root: root, Home: home, Config: &policy})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range result.Actions {
		if action.Path == role || action.Path == skill {
			t.Errorf("check plans %s %s, want the twin kept", action.Kind, action.Path)
		}
	}
	for _, want := range []string{agent + " did not build; output kept: " + role, command + " did not build; output kept: " + skill} {
		if !slices.Contains(result.Warnings, want) {
			t.Errorf("warnings %q, want %q", result.Warnings, want)
		}
	}
	for _, src := range []string{agent, command} {
		if !slices.ContainsFunc(result.Problems, func(p string) bool { return strings.HasPrefix(p, "FRONTMATTER-INVALID "+src+":") }) {
			t.Errorf("problems %q, want %s named FRONTMATTER-INVALID", result.Problems, src)
		}
	}
}
