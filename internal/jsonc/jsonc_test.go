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
	got, err := jsonc.SetProperty([]byte(config), "instructions", []byte(`["/a.md","/b.md"]`))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  // the operator's note\n  \"model\": \"x\", /* kept */\n  \"instructions\": [\"/a.md\",\"/b.md\"],\n}\n"
	if string(got) != want {
		t.Fatalf("set:\n%s\nwant:\n%s", got, want)
	}
	added, err := jsonc.SetProperty([]byte("{\n  // only a comment\n}\n"), "instructions", []byte(`["/b.md"]`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  // only a comment\n\n  \"instructions\": [\"/b.md\"]\n}\n"; string(added) != want {
		t.Fatalf("append:\n%s\nwant:\n%s", added, want)
	}
	if _, err := jsonc.DecodeObject(added); err != nil {
		t.Fatalf("appended document does not parse: %v", err)
	}
}

func TestRemovePropertyKeepsComments(t *testing.T) {
	got, err := jsonc.RemoveProperty([]byte(config), "instructions")
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  // the operator's note\n  \"model\": \"x\", /* kept */\n  \n}\n"
	if string(got) != want {
		t.Fatalf("remove:\n%q\nwant:\n%q", got, want)
	}
	document, err := jsonc.DecodeObject(got)
	if err != nil || document["model"] != "x" || document["instructions"] != nil {
		t.Fatalf("decode after remove: %v %v", document, err)
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
