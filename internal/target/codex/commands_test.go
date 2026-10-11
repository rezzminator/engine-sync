package codex

import (
	"github.com/rezzminator/engine-sync/internal/config"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontmatterAndRosterTransform(t *testing.T) {
	raw := "---\ndescription: >-\n  A quoted \\\"description\\\"\n  over two lines.\nmodel: opus\nhooks:\n  PostToolUse:\n    - matcher: Edit\n---\nUse /tools:go, not /tools or /scripts/x. Read CLAUDE.md.\n"
	fm, body, err := claudesource.Fields(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fm["description"], `A quoted \"description\" over two lines.`; got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
	got := transformMarkdown(body, TransformOptions{
		ModelMap:          map[string]string{"opus": "gpt-frontier"},
		Commands:          map[string]string{"tools:go": "tools-go"},
		ReplaceClaudeFile: true,
	})
	for _, want := range []string{"$tools-go", "/tools or", "/scripts/x", "AGENTS.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("transform missing %q in %q", want, got)
		}
	}
}

func TestCommandRewriteCoversDescriptionAndBody(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Project\nRead files.\n")
	writeCodexFixture(t, filepath.Join(root, ".claude", "commands", "hello.md"), "---\ndescription: old words.\n---\nold words.\n")
	policy := config.Default()
	policy.Rewrites = []config.Rewrite{{Engines: []string{"codex"}, Kinds: []string{"command"}, Literal: "old words", Replace: "new words"}}
	result, err := RunProject(Options{Root: root, Home: home, Config: &policy})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	raw := readCodexFixture(t, filepath.Join(root, ".codex", "skills", "hello", "SKILL.md"))
	if strings.Count(string(raw), "new words") != 2 {
		t.Fatal(raw)
	}
}
