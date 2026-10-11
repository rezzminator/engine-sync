package engine

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"testing"
)

func TestCommandsUsesMachineSnapshot(t *testing.T) {
	home := testjail.Scratch(t)
	called := false
	_, ok := Commands(home, "", config.Default(), []string{}, false, reconcile.Plan, func(p source.Project, c config.Config) ([]artifact.Artifact, []diag.Diagnostic) {
		called = true
		if p.Scope != source.MachineScope || !p.MachineCommands || p.SourceHome != home {
			t.Fatalf("wrong source: %#v", p)
		}
		return nil, nil
	}, func(source.Project, config.Config) []artifact.Root { return nil }, func([]artifact.Artifact) []diag.Diagnostic { return nil })
	if !called || !ok {
		t.Fatalf("called %v pass %v", called, ok)
	}
}
