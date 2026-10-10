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
