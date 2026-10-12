package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestTrackedSchemaMatchesLoader(t *testing.T) {
	raw, e := os.ReadFile(filepath.Join("..", "..", "docs", "engine-sync.schema.json"))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(raw, Schema()) {
		t.Fatal("tracked schema differs from loader's schema")
	}
}

// TestSchemaPassthroughKeysMatchTheLoader: the schema's passthrough-key
// pattern admits exactly the keys Validate accepts, at both layers.
func TestSchemaPassthroughKeysMatchTheLoader(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage
		Defs       map[string]struct{ Properties map[string]json.RawMessage } `json:"$defs"`
	}
	if err := json.Unmarshal(Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	for layer, agents := range map[string]json.RawMessage{"machine": schema.Properties["agents"], "project": schema.Defs["project"].Properties["agents"]} {
		var field struct {
			Properties struct {
				PassthroughKeys struct{ Items struct{ Pattern string } } `json:"passthroughKeys"`
			}
		}
		if err := json.Unmarshal(agents, &field); err != nil {
			t.Fatal(err)
		}
		pattern, err := regexp.Compile(field.Properties.PassthroughKeys.Items.Pattern)
		if err != nil || field.Properties.PassthroughKeys.Items.Pattern == "" {
			t.Fatalf("%s: passthroughKeys item pattern %q (%v)", layer, field.Properties.PassthroughKeys.Items.Pattern, err)
		}
		for _, key := range []string{"x-key", "color", "", "  ", "a b", "a:b", "a\tb", "a\nb", "\u0085", "\ufeff", "\u3000"} {
			cfg := Default()
			cfg.Agents.PassthroughKeys = []string{key}
			if schemaOK, loaderOK := pattern.MatchString(key), Validate(cfg) == nil; schemaOK != loaderOK {
				t.Errorf("%s: key %q schema=%v loader=%v", layer, key, schemaOK, loaderOK)
			}
		}
	}
}
