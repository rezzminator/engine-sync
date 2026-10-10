package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
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
