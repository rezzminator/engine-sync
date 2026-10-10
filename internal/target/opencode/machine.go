package opencode

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
)

func renderOpenCodeMachine(p source.Project, cfg config.Config) ([]artifact.Artifact, []diag.Diagnostic) {
	outputs := []artifact.Artifact{}
	ds := []diag.Diagnostic{}
	add := func(a artifact.Artifact) { outputs = append(outputs, openCodeArtifact(a)) }
	problem := func(f string, args ...any) {
		ds = append(ds, diag.Diagnostic{Severity: diag.Error, Message: fmt.Sprintf(f, args...)})
	}
	warn := func(f string, args ...any) {
		ds = append(ds, diag.Diagnostic{Severity: diag.Warning, Message: fmt.Sprintf(f, args...)})
	}
	dangling := func(path string, e error) {
		ds = append(ds, diag.Diagnostic{Severity: diag.Warning, State: reconcile.Dangling, Path: path, Message: fmt.Sprintf("%s %s: %v", reconcile.Dangling, path, e)})
	}
	for _, s := range p.Problems {
		problem("%s", s)
	}
	for _, s := range p.Warnings {
		warn("%s", s)
	}
	roster := map[string]string{}
	discoverOpenCodeCommandRoster(p.SourceCommandDir, roster, problem, dangling, &p)
	compileOpenCodeCommands(p.SourceCommandDir, "$HOME/.claude/commands", roster, filepath.Join(p.Home, ".config", openCodeName(), "command"), add, problem, warn, dangling, &p, cfg)
	return outputs, ds
}
