package paths_test

import (
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverRootAbsenceErrors(t *testing.T) {
	home := testjail.Scratch(t)
	child := filepath.Join(home, "unconfigured")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := paths.DiscoverRoot(child, home, "")
	want := "no project root found (expected configured marker, .git or CLAUDE.md)"
	if !errors.Is(fmt.Errorf("discover: %w", err), paths.ErrNoProjectRoot) || err.Error() != want {
		t.Fatalf("got %v, want ErrNoProjectRoot with text %q", err, want)
	}
	if err := os.WriteFile(filepath.Join(home, "CLAUDE.md"), []byte("# Home notes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := paths.DiscoverRoot(child, home, ""); !errors.Is(err, paths.ErrHomeRoot) {
		t.Fatalf("discovered HOME: got %v, want ErrHomeRoot", err)
	}
}

func TestProjectRootBoundaries(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, ".hidden")
	if err := os.Mkdir(home, 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, home} {
		_, err := paths.ProjectRoot(path, home)
		want := fmt.Sprintf("project root %s is HOME or an ancestor of HOME; select a repository", path)
		if !errors.Is(err, paths.ErrHomeRoot) || err.Error() != want {
			t.Errorf("HOME ancestor %s: got %v, want ErrHomeRoot with text %q", path, err, want)
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
