package source_test

import (
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
