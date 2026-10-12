package source_test

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkTargetReadsTheLinkTextAndNamesAnUnreadableLink(t *testing.T) {
	dir := testjail.Scratch(t)
	link := filepath.Join(dir, "labber.md")
	if err := os.Symlink("../vendor/lab/labber.md", link); err != nil {
		t.Fatal(err)
	}
	if got := source.LinkTarget(link); got != "../vendor/lab/labber.md" {
		t.Fatalf("LinkTarget = %q, want the link text", got)
	}
	if got := source.LinkTarget(filepath.Join(dir, "missing.md")); !strings.HasPrefix(got, "unreadable link (") {
		t.Fatalf("LinkTarget of a missing link = %q, want the read failure named", got)
	}
}

// A project agent or command whose header does not parse is a named problem
// of the loader, never a silent drop.
func TestLoadProjectNamesAHeaderThatDoesNotParse(t *testing.T) {
	root := testjail.Scratch(t)
	agent := filepath.Join(root, ".claude", "agents", "bad.md")
	command := filepath.Join(root, ".claude", "commands", "bad.md")
	for _, path := range []string{agent, command} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("---\ndescription: Reader\nengines:\n  codex: [unclosed\n---\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	p := source.LoadProject(root, filepath.Join(root, "home"), cfg)
	joined := strings.Join(p.Problems, "\n")
	for _, path := range []string{agent, command} {
		if !strings.Contains(joined, "FRONTMATTER-INVALID "+path+":") {
			t.Fatalf("problems %q, want %s named FRONTMATTER-INVALID", joined, path)
		}
	}
}
