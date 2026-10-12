package engine

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"strings"
)

// Execute runs every target and scope through the same artifact and diagnostic stages.
func Execute(p source.Project, cfg config.Config, mode reconcile.Mode,
	render func(source.Project, config.Config) ([]artifact.Artifact, []diag.Diagnostic),
	roots func(source.Project, config.Config) []artifact.Root,
	validate func([]artifact.Artifact) []diag.Diagnostic) (reconcile.Result, bool) {
	artifacts, diagnostics := render(p, cfg)
	result := reconcile.Result{}
	diagnostics = append(diagnostics, validate(artifacts)...)
	named := map[string]bool{}
	for _, d := range diagnostics {
		// The source loader and the target reading the same file name one
		// problem in the same words: it is one line.
		if named[d.Message] {
			continue
		}
		named[d.Message] = true
		if d.State == reconcile.Dangling {
			result.Dangling = append(result.Dangling, d.Message)
		}
		if d.Severity == diag.Warning && (mode == reconcile.Build || d.State == "") {
			message := d.Message
			if d.State == reconcile.Invalid {
				message = "SKIP " + message
			}
			result.Warnings = append(result.Warnings, message)
		} else {
			message := d.Message
			if d.Remedy != "" {
				message += " — " + d.Remedy
			}
			result.Problems = append(result.Problems, message)
		}
	}
	if mode == reconcile.Build && len(result.Problems) > 0 {
		return result, false
	}
	managed := roots(p, cfg)
	for i := range managed {
		if managed[i].Boundary != "" {
			continue
		}
		managed[i].Boundary = p.Root
		if p.Scope == source.MachineScope || !pathWithin(p.Root, managed[i].Path) {
			managed[i].Boundary = p.Home
			for _, base := range append(append([]string{}, p.CodexHomes...), p.ClaudeConfigDirs...) {
				if pathWithin(base, managed[i].Path) {
					managed[i].Boundary = base
					break
				}
			}
		}
	}
	reconciled := reconcile.Run(artifacts, mode, managed)
	result.Warnings = append(result.Warnings, reconciled.Warnings...)
	result.Problems = append(result.Problems, reconciled.Problems...)
	result.Rebuildable = reconciled.Rebuildable
	result.Wrote, result.Unchanged, result.Deleted = reconciled.Wrote, reconciled.Unchanged, reconciled.Deleted
	result.Actions = reconciled.Actions
	if mode != reconcile.Build {
		existing, problems := reconcile.Inspect(managed, artifacts)
		result.Problems = append(result.Problems, problems...)
		for _, d := range validate(existing) {
			if d.Severity == diag.Warning {
				result.Warnings = append(result.Warnings, d.Message)
			} else {
				result.Problems = append(result.Problems, d.Message)
			}
		}
	}
	return result, len(result.Problems) == 0
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
