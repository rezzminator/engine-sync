package opencode

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"strings"
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

// MachineAgentsDir is OpenCode's global agent directory,
// {home}/.config/opencode/agents (https://opencode.ai/docs/agents/).
func MachineAgentsDir(home string) string {
	return filepath.Join(home, ".config", openCodeName(), "agents")
}

// renderOpenCodeMachineAgents emits every machine-global agent, original and
// variant, into MachineAgentsDir through the one OpenCode agent renderer. The
// machine path carries no preamble: no project AGENTS.md sits beside it.
// Source problems are the Codex pass's to report, never repeated here.
func renderOpenCodeMachineAgents(p source.Project, cfg config.Config) ([]artifact.Artifact, []diag.Diagnostic) {
	outputs := []artifact.Artifact{}
	ds := []diag.Diagnostic{}
	add := func(a artifact.Artifact) { outputs = append(outputs, openCodeArtifact(a)) }
	problem := func(f string, args ...any) {
		ds = append(ds, diag.Diagnostic{Severity: diag.Error, Message: fmt.Sprintf(f, args...)})
	}
	warn := func(f string, args ...any) {
		ds = append(ds, diag.Diagnostic{Severity: diag.Warning, Message: fmt.Sprintf(f, args...)})
	}
	dir := MachineAgentsDir(p.Home)
	for _, name := range p.Failed {
		// An agent skipped for an error keeps its installed copy.
		add(artifact.Artifact{Path: filepath.Join(dir, name+".md"), Kept: &artifact.SourceLink{Path: name}})
	}
	for _, a := range p.Agents {
		name := a.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(a.Path), ".md")
		}
		renderOpenCodeAgent(openCodeAgentSpec{
			Name: name, Path: a.Path, Generated: a.Generated,
			Marker: source.MachineMarkerSource(a.Path, p.AgentSourceDir, cfg.Machine.AgentMarkerDir),
			Output: filepath.Join(dir, name+".md"),
		}, a.Front, a.Body, map[string]string{}, cfg.OpenCode.ModelMap, nil, cfg.Rewrites, add, problem, warn, cfg)
	}
	return outputs, ds
}
