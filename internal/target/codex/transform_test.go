package codex

import (
	"testing"
)

// Door 1: transformMarkdown, the transform every compiled project command,
// agent body, agent description and AGENTS.md passes through.
func TestTransformMarkdownRewritesCodeReview(t *testing.T) {
	got := transformMarkdown("Read CLAUDE.md, then `/code-review high`.", TransformOptions{
		ModelMap:          map[string]string{"opus": "gpt-frontier"},
		ReplaceClaudeFile: true,
	})
	want := "Read AGENTS.md, then `" + codexReviewShellCommand("gpt-frontier", "high") + "`."
	if got != want {
		t.Fatalf("transformMarkdown = %q, want %q", got, want)
	}
}
