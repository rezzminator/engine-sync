package engine

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestPipelinePlansBeforePublishingAndValidates(t *testing.T) {
	root := testjail.Scratch(t)
	path := filepath.Join(root, "role.toml")
	render := func(source.Project, config.Config) ([]artifact.Artifact, []diag.Diagnostic) {
		return []artifact.Artifact{{Path: path, Content: []byte(artifact.Marker(artifact.TOML, "reader.md") + "\nname = \"reader\"\n")}}, nil
	}
	roots := func(source.Project, config.Config) []artifact.Root { return []artifact.Root{{Path: root}} }
	validate := func([]artifact.Artifact) []diag.Diagnostic { return nil }
	r, _ := Execute(source.Project{}, config.Default(), reconcile.Plan, render, roots, validate)
	if len(r.Actions) != 1 {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("plan mutated disk")
	}
	r, ok := Execute(source.Project{}, config.Default(), reconcile.Build, render, roots, validate)
	if !ok || r.Wrote != 1 {
		t.Fatalf("%+v", r)
	}
	r, ok = Execute(source.Project{}, config.Default(), reconcile.Check, render, roots, validate)
	if !ok || r.Unchanged != 1 {
		t.Fatalf("%+v", r)
	}
}
