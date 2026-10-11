package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingStateNamesRepairableArtifact(t *testing.T) {
	path := filepath.Join(testjail.Scratch(t), "role.md")
	r := reconcile.Run([]artifact.Artifact{{Path: path, Content: []byte(artifact.Marker(artifact.Markdown, "source"))}}, reconcile.Check, nil)
	if len(r.Problems) != 1 || len(r.Rebuildable) != 1 || !strings.HasPrefix(r.Problems[0], string(reconcile.Missing)+" ") {
		t.Fatalf("%+v", r)
	}
}
