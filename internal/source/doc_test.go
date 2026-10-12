package source

import (
	"testing"
)

func TestFrontmatterEngineOverridesAndScalarForms(t *testing.T) {
	text := "---\ndescription: >\n  Read a file\n  and return a quote.\nmodel: opus # alias\ntools:\n  - Read\n  - Bash\nengines:\n  codex:\n    sandbox: inherit\n  opencode:\n    model: selected\n---\nBody.\n"
	front, body, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if front.Description != "Read a file and return a quote." || front.Model != "opus" || len(front.Tools) != 2 || front.Engines.Codex.Sandbox != "inherit" || front.Engines.OpenCode.Model != "selected" || body != "Body.\n" {
		t.Fatalf("front=%#v body=%q", front, body)
	}
}
