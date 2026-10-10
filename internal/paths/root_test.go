package paths_test

import (
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRootBoundaries(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, ".hidden")
	if err := os.Mkdir(home, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, home} {
		if _, err := paths.ProjectRoot(path, home); err == nil {
			t.Errorf("admitted HOME ancestor %s", path)
		}
	}
	child := filepath.Join(root, "repo")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	if got, err := paths.ProjectRoot(child, home); err != nil || got != child {
		t.Fatalf("got %s error %v", got, err)
	}
}
