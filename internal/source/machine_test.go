package source

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestMachineSourcesRemainFrozenAfterRemoval(t *testing.T) {
	root := testjail.Scratch(t)
	path := filepath.Join(root, "reader.md")
	raw := []byte("---\nname: reader\ndescription: Read.\n---\nRead files.\n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadMachine(root, root, filepath.Join(root, "generated"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got, err := p.Read(path)
	if err != nil || string(got) != string(raw) || len(p.Agents) != 1 {
		t.Fatalf("%q %+v %v", got, p.Agents, err)
	}
}

func TestMachineDeclarationsExcludeAbstracts(t *testing.T) {
	root := testjail.Scratch(t)
	for file, text := range map[string]string{
		"reader.md":  "---\nname: reader\ndescription: Read.\n---\nRead.\n",
		"layer.md":   "---\nname: layer\nabstract: true\nmodel: sonnet\n---\n",
		"derived.md": "---\nname: derived\nfrom: reader\nextends: [layer]\n---\n",
	} {
		if err := os.WriteFile(filepath.Join(root, file), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	model, err := LoadMachine(root, root, filepath.Join(root, "generated"))
	if err != nil || len(model.Problems) != 0 || len(model.Agents) != 2 {
		t.Fatalf("model = %+v (%v)", model, err)
	}
	for _, agent := range model.Agents {
		if agent.Name == "derived" {
			if !agent.Generated || agent.Front.Model != "sonnet" {
				t.Fatalf("derived = %+v", agent)
			}
			return
		}
	}
	t.Fatal("derived agent not rendered")
}

// TestMachineBrokenAbstractIsAProblemOnly: an abstract whose header does not
// parse is named as a problem and never listed as a failed agent, since it
// has no installed copy to keep.
func TestMachineBrokenAbstractIsAProblemOnly(t *testing.T) {
	root := testjail.Scratch(t)
	for name, content := range map[string]string{
		"layer.md":  "---\nname: layer\nabstract: true\nengines:\n\tcodex: false\n---\n",
		"reader.md": "---\nname: reader\ndescription: Read.\n---\nRead files.\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := LoadMachine(root, root, filepath.Join(root, "generated"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) == 0 {
		t.Fatal("broken abstract: no problem named")
	}
	if len(p.Failed) != 0 {
		t.Fatalf("Failed = %q, want no failed agent for a broken abstract", p.Failed)
	}
	if !p.Declares {
		t.Fatal("a broken abstract still marks the directory as declaring")
	}
}
