package jsonc_test

import (
	"testing"

	"github.com/rezzminator/engine-sync/internal/jsonc"
)

const config = `{
  // the operator's note
  "model": "x", /* kept */
  "instructions": ["/a.md"],
}
`

func TestSetPropertyKeepsComments(t *testing.T) {
	for _, row := range []struct{ name, raw, value, want string }{
		{"replace", config, `["/a.md","/b.md"]`,
			"{\n  // the operator's note\n  \"model\": \"x\", /* kept */\n  \"instructions\": [\"/a.md\",\"/b.md\"],\n}\n"},
		{"append after a comment", "{\n  // only a comment\n}\n", `["/b.md"]`,
			"{\n  // only a comment\n\n  \"instructions\": [\"/b.md\"]\n}\n"},
		{"append after a string holding a comma", "{\n  \"model\": \"a, }b\" // tail\n}\n", `["/b.md"]`,
			"{\n  \"model\": \"a, }b\", // tail\n\n  \"instructions\": [\"/b.md\"]\n}\n"},
	} {
		got, err := jsonc.SetProperty([]byte(row.raw), "instructions", []byte(row.value))
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		if string(got) != row.want {
			t.Fatalf("%s:\n%q\nwant:\n%q", row.name, got, row.want)
		}
		if _, err := jsonc.DecodeObject(got); err != nil {
			t.Fatalf("%s: the result does not parse: %v", row.name, err)
		}
	}
}

func TestRemovePropertyKeepsComments(t *testing.T) {
	for _, row := range []struct{ name, raw, remove, want string }{
		{"last with trailing comma", config, "instructions",
			"{\n  // the operator's note\n  \"model\": \"x\", /* kept */\n  \n}\n"},
		{"last without trailing comma", "{\n  \"a\": 1 /* one */, // two\n  \"b\": 2\n}\n", "b",
			"{\n  \"a\": 1 /* one */ // two\n  \n}\n"},
		{"first beside a string holding a comma", "{\n  \"a\": 1,\n  \"b\": \"x, }\"\n}\n", "a",
			"{\n  \n  \"b\": \"x, }\"\n}\n"},
	} {
		got, err := jsonc.RemoveProperty([]byte(row.raw), row.remove)
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		if string(got) != row.want {
			t.Fatalf("%s:\n%q\nwant:\n%q", row.name, got, row.want)
		}
		if _, err := jsonc.DecodeObject(got); err != nil {
			t.Fatalf("%s: the result does not parse: %v", row.name, err)
		}
	}
}

func TestDecodeObjectRefusesDuplicateKeys(t *testing.T) {
	if _, err := jsonc.DecodeObject([]byte(`{"a":1,"a":2}`)); err == nil {
		t.Fatal("a duplicate key decoded")
	}
}

func TestDecodeObjectKeepsCommasInStrings(t *testing.T) {
	document, err := jsonc.DecodeObject([]byte(`{"instructions": ["/notes, ]x.md", "/b, }.md",],}`))
	if err != nil {
		t.Fatal(err)
	}
	items, _ := document["instructions"].([]any)
	if len(items) != 2 || items[0] != "/notes, ]x.md" || items[1] != "/b, }.md" {
		t.Fatalf("decoded %v", items)
	}
}
