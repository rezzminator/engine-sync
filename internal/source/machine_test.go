package source

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
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

// TestMachineSharedFilesAreNeverAgents: a shared file below a subdirectory
// layers into the declaration that extends it by relative path, and is never
// an agent of its own.
func TestMachineSharedFilesAreNeverAgents(t *testing.T) {
	root := testjail.Scratch(t)
	for file, text := range map[string]string{
		"reader.md":       "---\nname: reader\ndescription: Read.\n---\nRead.\n",
		"shared/layer.md": "---\nmodel: sonnet\n---\n",
		"derived.md":      "---\nname: derived\nfrom: reader\nextends: [shared/layer.md]\n---\n",
	} {
		writeSourceFixture(t, filepath.Join(root, file), text)
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

// TestMachineBrokenSharedFileIsAProblemOnly: a shared file whose header does
// not parse is named as a problem and never listed as a failed agent, since
// it has no installed copy to keep.
func TestMachineBrokenSharedFileIsAProblemOnly(t *testing.T) {
	root := testjail.Scratch(t)
	for name, content := range map[string]string{
		"shared/layer.md": "---\nengines:\n\tcodex: false\n---\n",
		"reader.md":       "---\nname: reader\ndescription: Read.\n---\nRead files.\n",
	} {
		writeSourceFixture(t, filepath.Join(root, name), content)
	}
	p, err := LoadMachine(root, root, filepath.Join(root, "generated"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(p.Problems, "\n"), "layer.md") {
		t.Fatalf("broken shared file: problems %q do not name it", p.Problems)
	}
	if len(p.Failed) != 0 {
		t.Fatalf("Failed = %q, want no failed agent for a broken shared file", p.Failed)
	}
}

func writeSourceFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
