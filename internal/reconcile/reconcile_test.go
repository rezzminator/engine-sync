package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestReconcilerPlansOwnedOrphanThenRemovesIt(t *testing.T) {
	root := testjail.Scratch(t)
	path := filepath.Join(root, "old.md")
	if err := os.WriteFile(path, []byte(artifact.Marker(artifact.Markdown, "source")), 0600); err != nil {
		t.Fatal(err)
	}
	roots := []artifact.Root{{Path: root}}
	plan := reconcile.Run(nil, reconcile.Plan, roots)
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "delete" {
		t.Fatalf("%+v", plan)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	built := reconcile.Run(nil, reconcile.Build, roots)
	if built.Deleted != 1 {
		t.Fatalf("%+v", built)
	}
}
