package codex

import (
	"testing"
)

// Door 1: transformMarkdown, the transform every compiled project command,
// agent body, agent description and AGENTS.md passes through.
func TestTransformMarkdownRewritesCodeReview(t *testing.T) {
	got := transformMarkdown("Read CLAUDE.md, then `/code-review high`.", TransformOptions{
		Review:            reviewFixturePolicy,
		ModelMap:          map[string]string{codeReviewModelAlias: "gpt-frontier"},
		ReplaceClaudeFile: true,
	})
	want := "Read AGENTS.md, then `" + codexReviewShellCommand("gpt-frontier", "high", reviewFixturePolicy) + "`."
	if got != want {
		t.Fatalf("transformMarkdown = %q, want %q", got, want)
	}
}
