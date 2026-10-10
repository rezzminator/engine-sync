package artifact

import (
	"strings"
	"testing"
)

func TestMarkerSyntaxAndLegacyClaims(t *testing.T) {
	for _, syntax := range []Syntax{Markdown, TOML, JSONC, YAMLFront} {
		for _, source := range []string{".claude/agents/reader.md", "CLAUDE.md", "nested/.claude/commands/review.md"} {
			if got := Marker(syntax, source); !Claims(got) || !strings.Contains(got, "engine-sync build") {
				t.Fatal(got)
			}
		}
		got := Marker(syntax, ".claude/agents/reader.md")
		for _, legacy := range []string{LegacyCodex, LegacyOpenCode, LegacyVariant, LegacyCodexScript, LegacyOpenCodeScript} {
			if strings.Contains(got, legacy) {
				t.Fatal(got)
			}
		}
	}
	for _, legacy := range Markers() {
		if !Claims("# " + legacy + " source") {
			t.Fatal(legacy)
		}
	}
	if Claims("# User file\n" + Marker(TOML, "source")) {
		t.Fatal("a body quote claimed the file")
	}
}
