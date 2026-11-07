package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestKeepArtifactNamesAnOutputItCannotInspect(t *testing.T) {
	dir := testjail.Scratch(t)
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A path under a regular file fails Lstat with ENOTDIR, not ENOENT, even as root.
	warning, problem := reconcile.KeepArtifact(filepath.Join(file, "twin"), "/home/test/src.md", "../gone.md")
	if warning != "" || !strings.HasPrefix(problem, "inspect kept output ") {
		t.Fatalf("uninspectable twin: warning=%q problem=%q", warning, problem)
	}
}

// TestExpandKeptReservesAPrefixButNotALongerOne: a failed plugin's "p-"
// reservation keeps each "p-…" twin, never one of a configured plugin
// named "p-…" whose twins are its own.
func TestExpandKeptReservesAPrefixButNotALongerOne(t *testing.T) {
	dir := testjail.Scratch(t)
	for _, name := range []string{"tool-lander.toml", "tool-visibility-scan.toml", "tools.toml", "other.toml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reserve := artifact.Artifact{Path: filepath.Join(dir, "tool-"), Kept: &artifact.SourceLink{Path: "plugin tool", Prefix: "tool-", Except: []string{"tool-visibility-"}}}
	expanded, problems := reconcile.ExpandKept([]artifact.Artifact{reserve})
	var kept []string
	for _, a := range expanded {
		kept = append(kept, filepath.Base(a.Path))
	}
	if want := []string{"tool-lander.toml"}; !reflect.DeepEqual(kept, want) || len(problems) != 0 {
		t.Fatalf("kept %q problems %q, want %q and none", kept, problems, want)
	}
}
