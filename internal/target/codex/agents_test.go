package codex

import (
	"github.com/rezzminator/engine-sync/internal/config"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"strings"
	"testing"
)

func TestNeutralRoleUsesEngineModelDefault(t *testing.T) {
	role, warning, e := codexRoleSettings(claudesource.Frontmatter{Model: "sonnet", Fields: map[string]string{"model": "sonnet"}}, config.Default().Codex.ModelMap, "role.md")
	if e != nil || role.Model != "" || !strings.Contains(warning, `unmapped Codex model alias "sonnet"`) {
		t.Fatalf("unmapped neutral role=%#v err=%v", role, e)
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
