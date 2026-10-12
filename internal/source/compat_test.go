package source

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// A header that fails strict YAML is retried once with each top-level
// single-line value that does not parse on its own quoted as a literal; a
// header that still fails is FRONTMATTER-INVALID.
func TestHeaderCompatibilityRetry(t *testing.T) {
	cases := []struct {
		name, header, key string
		kind              yaml.Kind
		value             string
		invalid           bool
	}{
		{name: "colon in a plain value", header: "description: Review: run the checks", key: "description", kind: yaml.ScalarNode, value: "Review: run the checks"},
		{name: "two flow lists", header: "description: Reader\nargument-hint: [pr-number] [priority]", key: "argument-hint", kind: yaml.ScalarNode, value: "[pr-number] [priority]"},
		{name: "one flow list stays a list", header: "argument-hint: [pr-number]", key: "argument-hint", kind: yaml.SequenceNode},
		{name: "nested engines block beside a retried line", header: "description: Review: run the checks\nengines:\n  codex:\n    model: gpt\n", key: "engines", kind: yaml.MappingNode},
		{name: "broken nested block", header: "description: Reader\nengines:\n  codex: [unclosed\n", invalid: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node, err := ParseHeader(c.header)
			if c.invalid {
				if got := DescribeParseError("a.md", err); !strings.HasPrefix(got, "FRONTMATTER-INVALID a.md:") {
					t.Fatalf("ParseHeader(%q) = %v, want FRONTMATTER-INVALID", c.header, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHeader(%q): %v", c.header, err)
			}
			var value *yaml.Node
			for i := 0; i+1 < len(node.Content); i += 2 {
				if node.Content[i].Value == c.key {
					value = node.Content[i+1]
				}
			}
			if value == nil || value.Kind != c.kind || (c.kind == yaml.ScalarNode && value.Value != c.value) {
				t.Fatalf("ParseHeader(%q) %s = %+v, want kind %v value %q", c.header, c.key, value, c.kind, c.value)
			}
		})
	}
	front, _, err := Parse("---\ndescription: Review: run the checks\nengines:\n  codex:\n    model: gpt\n---\nbody")
	if err != nil || front.Description != "Review: run the checks" || front.Engines.Codex == nil || front.Engines.Codex.Model != "gpt" {
		t.Fatalf("Parse = %+v, %v", front, err)
	}
	if _, _, err := Parse("---\ndescription: Reader\nbody"); !strings.HasPrefix(DescribeParseError("a.md", err), "FRONTMATTER-INVALID a.md:1 — ") {
		t.Fatalf("an unclosed header = %q, want FRONTMATTER-INVALID", DescribeParseError("a.md", err))
	}
}
