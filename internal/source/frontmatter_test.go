package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestReviewFrontmatterForms(t *testing.T) {
	for _, text := range []string{"---\r\ndescription: Reader\r\ntools: [Read, Write]\r\n---\r\nbody", "\ufeff--- \ndescription: Reader\ntools:\n  - 'Read'\n  - \"Write\"\n--- \nbody"} {
		f, body, e := Parse(text)
		if e != nil || f.Description != "Reader" || len(f.Tools) != 2 || f.Tools[1] != "Write" || body != "body" {
			t.Fatalf("frontmatter %+v body %q error %v", f, body, e)
		}
	}
}

// An empty required capability is never met, whatever the host carries.
func TestUnmetNeverMeetsAnEmptyName(t *testing.T) {
	if got := Unmet([]string{"", "gpt-agents"}, []string{"gpt-agents"}); got != `""` {
		t.Fatalf("Unmet = %q, want the empty name named", got)
	}
	if got := Unmet([]string{"gpt-agents"}, []string{"gpt-agents"}); got != "" {
		t.Fatalf("Unmet = %q, want met", got)
	}
}

// TestAgentSchemaRejectsEmptyRequiresAndModels: docs/agent.schema.json
// refuses what ValidateAgent refuses — an empty or blank requires item and an
// empty or blank engine model — and admits a name.
func TestAgentSchemaRejectsEmptyRequiresAndModels(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "agent.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	type str struct{ Pattern string }
	type engine struct {
		OneOf []struct{ Properties struct{ Model str } } `json:"oneOf"`
	}
	var schema struct {
		Properties struct {
			Requires struct{ Items str }
			Engines  struct {
				Properties struct{ Codex, OpenCode engine }
			}
		}
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{"requires item": schema.Properties.Requires.Items.Pattern}
	for name, e := range map[string]engine{"codex": schema.Properties.Engines.Properties.Codex, "opencode": schema.Properties.Engines.Properties.OpenCode} {
		if len(e.OneOf) != 2 {
			t.Fatalf("engines.%s: oneOf = %d alternatives, want false or a block", name, len(e.OneOf))
		}
		fields["engines."+name+".model"] = e.OneOf[1].Properties.Model.Pattern
	}
	for field, pattern := range fields {
		re, err := regexp.Compile(pattern)
		if err != nil || pattern == "" {
			t.Fatalf("%s: pattern %q (%v)", field, pattern, err)
		}
		for value, want := range map[string]bool{"gpt-agents": true, "": false, "  ": false} {
			if re.MatchString(value) != want {
				t.Errorf("%s: %q admitted=%v, want %v", field, value, !want, want)
			}
		}
	}
}
