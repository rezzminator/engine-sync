package source

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"slices"
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
		// A declaration without name is named by its file base.
		"bare.md": "---\nfrom: reader\n---\n",
	} {
		writeSourceFixture(t, filepath.Join(root, file), text)
	}
	model, err := LoadMachine(root, root, filepath.Join(root, "generated"))
	if err != nil || len(model.Problems) != 0 || len(model.Agents) != 3 {
		t.Fatalf("model = %+v (%v)", model, err)
	}
	rendered := map[string]Agent{}
	for _, agent := range model.Agents {
		rendered[agent.Name] = agent
	}
	if derived := rendered["derived"]; !derived.Generated || derived.Front.Model != "sonnet" {
		t.Fatalf("derived = %+v", derived)
	}
	if bare := rendered["bare"]; !bare.Generated || bare.Front.Fields["name"] != "bare" || bare.Front.Fields["description"] != "Read." {
		t.Fatalf("bare = %+v, want reader rendered under the name bare", bare)
	}
}

// TestMachineUnreadableSourceIsAProblemOnly: a source that cannot be read,
// and a shared subdirectory that cannot be listed, are each named; the
// source keeps its installed copy, and every other agent still loads.
func TestMachineUnreadableSourceIsAProblemOnly(t *testing.T) {
	if testjail.RanUnprivileged(t) {
		return
	}
	root := testjail.Scratch(t)
	for name, content := range map[string]string{
		"reader.md":        "---\nname: reader\ndescription: Read.\n---\nRead files.\n",
		"derived.md":       "---\nname: derived\nfrom: reader\n---\n",
		"locked/layer.md":  "---\nmodel: sonnet\n---\n",
		"odd.md/README.md": "Not an agent.\n",
	} {
		writeSourceFixture(t, filepath.Join(root, name), content)
	}
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	p, err := LoadMachine(root, root, filepath.Join(root, "generated"))
	if err != nil {
		t.Fatalf("LoadMachine = %v, want the unreadable sources named and the rest loaded", err)
	}
	odd := filepath.Join(root, "odd.md")
	want := []string{
		"read " + odd + ": read " + odd + ": not a regular file",
		"VARIANT-INVALID read shared directory " + locked + ": open " + locked + ": permission denied",
	}
	if !slices.Equal(p.Problems, want) || !slices.Equal(p.Failed, []string{"odd"}) {
		t.Fatalf("problems %q failed %q, want %q and [odd]", p.Problems, p.Failed, want)
	}
	var names []string
	for _, agent := range p.Agents {
		names = append(names, agent.Name)
	}
	if !slices.Equal(names, []string{"reader", "derived"}) {
		t.Fatalf("agents %q, want reader and derived", names)
	}
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
