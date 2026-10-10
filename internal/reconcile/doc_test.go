package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestKeepArtifactWarnsOnAnExistingTwinAndIsSilentWithoutOne(t *testing.T) {
	dir := testjail.Scratch(t)
	twin := filepath.Join(dir, "labber.toml")
	warning, problem := reconcile.KeepArtifact(twin, "/home/test/src.md", "../gone.md")
	if warning != "" || problem != "" {
		t.Fatalf("absent twin: warning=%q problem=%q, want neither", warning, problem)
	}
	if err := os.WriteFile(twin, []byte("twin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	warning, problem = reconcile.KeepArtifact(twin, "/home/test/src.md", "../gone.md")
	if warning != "source unresolvable: /home/test/src.md → ../gone.md; output kept" || problem != "" {
		t.Fatalf("existing twin: warning=%q problem=%q", warning, problem)
	}
}
