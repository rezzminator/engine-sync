package machine

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyGlobalLinkAliasRoot(t *testing.T) {
	root := testjail.Scratch(t)
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, foundRoot, sourceRoot string }{
		{"alias target", alias, repo}, {"alias root", repo, alias},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := filepath.Join(root, tc.name)
			stale := filepath.Join(tc.foundRoot, "stale.md")
			if err := os.Symlink(stale, target); err != nil {
				t.Fatal(err)
			}
			state, _, err := ClassifyGlobalLink(
				target, filepath.Join(tc.sourceRoot, "desired.md"), tc.sourceRoot, GlobalLinkFile,
			)
			if err != nil || state != GlobalLinkConflict {
				t.Fatalf("state=%s err=%v", state, err)
			}
		})
	}
}
