package source

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownDiscoverySortsAndExcludesSubtrees(t *testing.T) {
	root := testjail.Scratch(t)
	for _, name := range []string{"b.md", "a.md", "README.md", "skip/c.md"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("body"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fail := func(s string) { t.Fatal(s) }
	got := Markdown(root, []string{"skip"}, Reporter{Problem: fail, Warning: fail, Dangling: func(p, r string, e error) { t.Fatal(e) }})
	if len(got) != 2 || got[0].Rel != "a.md" || got[1].Rel != "b.md" {
		t.Fatalf("%+v", got)
	}
}
