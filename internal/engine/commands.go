package engine

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
)

func Commands(home, sourceHome string, cfg config.Config, homes []string, uninstall bool, mode reconcile.Mode, render func(source.Project, config.Config) ([]artifact.Artifact, []diag.Diagnostic), roots func(source.Project, config.Config) []artifact.Root, validate func([]artifact.Artifact) []diag.Diagnostic) (reconcile.Result, bool) {
	if sourceHome == "" {
		sourceHome = home
	}
	p := source.LoadMachineCommands(home, sourceHome, uninstall, cfg)
	p.CodexHomes = homes
	return Execute(p, cfg, mode, render, roots, validate)
}
