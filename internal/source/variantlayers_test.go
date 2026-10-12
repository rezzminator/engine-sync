package source

import (
	"bytes"
	"encoding/json"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestJSONNodeKeepsScalarTypes: a variants.json value rendered into a
// variant's frontmatter reads back as the same JSON type — a string YAML
// would retype (a bool, a number, null, a key-value, a comment) stays a
// string, and a JSON number or bool stays one.
func TestJSONNodeKeepsScalarTypes(t *testing.T) {
	for _, value := range []any{
		"true", "no", "on", "null", "~", "1.0", "0123", "0x1F", "1e3", "a: b", "x # y", "- item", "[a] [b]", "{brace}",
		"*alias", "&anchor", "!tag", "%percent", "|pipe", ">gt", "@at", "`tick`", "'single'", "\"double\"", " lead", "trail ",
		"two\nlines", "", 30, 1.5, true, false,
	} {
		raw, err := json.Marshal(map[string]any{"key": value})
		if err != nil {
			t.Fatal(err)
		}
		node, err := jsonNode(raw)
		if err != nil {
			t.Fatalf("jsonNode(%s): %v", raw, err)
		}
		var out bytes.Buffer
		enc := yaml.NewEncoder(&out)
		enc.SetIndent(2)
		if err := enc.Encode(node); err != nil {
			t.Fatal(err)
		}
		if err := enc.Close(); err != nil {
			t.Fatal(err)
		}
		var back map[string]any
		if err := yaml.Unmarshal(out.Bytes(), &back); err != nil {
			t.Fatalf("%q renders as unreadable YAML %q: %v", value, out.String(), err)
		}
		want := value
		if n, ok := value.(int); ok {
			want = n
		}
		if got := back["key"]; got != want {
			t.Fatalf("%#v renders as %q and reads back as %#v", value, out.String(), got)
		}
	}
}
