package source

import (
	"bytes"

	"testing"

	"go.yaml.in/yaml/v3"
)

// Declaration settings retain scalar types when canonical YAML is emitted.
func TestCanonicalNodeKeepsScalarTypes(t *testing.T) {
	for _, value := range []any{
		"true", "no", "on", "null", "~", "1.0", "0123", "0x1F", "1e3", "a: b", "x # y", "- item", "[a] [b]", "{brace}",
		"*alias", "&anchor", "!tag", "%percent", "|pipe", ">gt", "@at", "`tick`", "'single'", "\"double\"", " lead", "trail ",
		"two\nlines", "", 30, 1.5, true, false,
	} {
		raw, err := yaml.Marshal(map[string]any{"key": value})
		if err != nil {
			t.Fatal(err)
		}
		node, err := ParseHeader(string(raw))
		if err != nil {
			t.Fatalf("ParseHeader(%s): %v", raw, err)
		}
		var out bytes.Buffer
		enc := yaml.NewEncoder(&out)
		enc.SetIndent(2)
		if err := enc.Encode(canonicalNode(node)); err != nil {
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
