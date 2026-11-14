package engine

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
)

func Commands(home, sourceHome string, cfg config.Config, homes, claudeConfigDirs []string, uninstall bool, mode reconcile.Mode, render func(source.Project, config.Config) ([]artifact.Artifact, []diag.Diagnostic), roots func(source.Project, config.Config) []artifact.Root, validate func([]artifact.Artifact) []diag.Diagnostic) (reconcile.Result, bool) {
	if sourceHome == "" {
		sourceHome = home
	}
	p := source.LoadMachineCommands(home, sourceHome, uninstall, claudeConfigDirs, cfg)
	p.CodexHomes = homes
	return Execute(p, cfg, mode, render, roots, validate)
}

// GlobalCommands is the optional global-commands step a project run carries:
// the machine policy and targets of home, then Commands. A policy or targets
// that do not resolve fail only this step, as one problem, never the project
// result it joins.
func GlobalCommands(home string, mode reconcile.Mode, render func(source.Project, config.Config) ([]artifact.Artifact, []diag.Diagnostic), roots func(source.Project, config.Config) []artifact.Root, validate func([]artifact.Artifact) []diag.Diagnostic) (reconcile.Result, bool) {
	machine, err := config.Resolve("", home, paths.Getenv)
	if err == nil {
		var roster config.Targets
		if roster, err = config.ResolveTargets(paths.Getenv, home, machine, config.Targets{}); err == nil {
			return Commands(home, "", machine, *roster.CodexHomes, roster.ClaudeConfigDirs, false, mode, render, roots, validate)
		}
	}
	return reconcile.Result{Problems: []string{"global commands: " + err.Error()}}, false
}
