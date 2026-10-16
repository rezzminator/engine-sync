package opencode

import (
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"testing"
)

func TestParseOpenCodeFrontmatterReadsAYAMLListAsTheCommaJoinedList(t *testing.T) {
	listed, _, err := parseFields(
		"---\ndescription: Worker.\ntools:\n  - Read\n  - Grep\nmodel: sonnet\n---\nWork.\n",
	)
	if err != nil {
		t.Fatalf("parse list: %v", err)
	}
	inline, _, err := parseFields(
		"---\ndescription: Worker.\ntools: Read, Grep\nmodel: sonnet\n---\nWork.\n",
	)
	if err != nil {
		t.Fatalf("parse inline: %v", err)
	}
	if listed["tools"] != "Read, Grep" || listed["model"] != "sonnet" {
		t.Fatalf("list fields = %q, want tools %q and model sonnet", listed, "Read, Grep")
	}
	listedBlock, _, _ := renderOpenCodeToolsBlock("worker.md", listed["tools"], []string{"chat-professor", "harvester-professor"})
	inlineBlock, _, _ := renderOpenCodeToolsBlock("worker.md", inline["tools"], []string{"chat-professor", "harvester-professor"})
	if listedBlock != inlineBlock {
		t.Fatalf("list block = %q, inline block = %q", listedBlock, inlineBlock)
	}
}

func parseFields(text string) (map[string]string, string, error) {
	front, body, err := claudesource.Parse(text)
	return front.Fields, body, err
}
