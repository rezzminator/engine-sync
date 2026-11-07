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
	for _, failure := range p.PluginFailures {
		// A plugin that could not be read keeps every "/{plugin}:…" on its
		// flattened twin name (rewrite.CommandsIn's prefix key).
		roster[failure.Plugin+":"] = "/" + source.PluginTwinName(failure.Plugin, "")
	}
	for _, plugin := range p.Plugins {
		for _, entry := range pluginCommandEntries(plugin) {
			roster[source.CommandName(entry.Rel)] = "/" + openCodeFlatName(entry.Rel)
		}
	}
	output := filepath.Join(p.Home, ".config", openCodeName(), "command")
	compileOpenCodeCommands(p.SourceCommandDir, "$HOME/.claude/commands", roster, output, add, problem, warn, dangling, &p, cfg.Machine.ManagedBy, cfg)
	// A plugin's commands and skills are OpenCode commands, as machine skills
	// are; each credits its plugin and takes its root.
	for _, plugin := range p.Plugins {
		for _, entry := range pluginCommandEntries(plugin) {
			file := entry.Path
			if entry.SkillDir {
				file = filepath.Join(file, "SKILL.md")
			}
			from := openCodeCommandSource{Roster: roster, Agents: p.AgentRoster, PluginRoot: plugin.Root, Marker: source.PluginMarkerSource(&plugin, file)}
			compileOpenCodeCommand(entry, from, output, add, problem, dangling, &p, cfg.Machine.ManagedBy, cfg)
		}
	}
	for _, failure := range p.PluginFailures {
		kept := failure.Kept(output)
		add(kept)
		ds = append(ds, diag.PluginFailed(failure, kept.Path))
	}
	return outputs, ds
}

// MachineAgentsDir is OpenCode's global agent directory,
// {home}/.config/opencode/agents (https://opencode.ai/docs/agents/).
func MachineAgentsDir(home string) string {
	return filepath.Join(home, ".config", openCodeName(), "agents")
}

// renderOpenCodeMachineAgents emits every machine-global agent, original and
// generated agent, into MachineAgentsDir through the one OpenCode agent renderer. The
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
	known := knownMCPServers(nil, "", cfg.MCP)
	for _, failure := range p.PluginFailures {
		kept := failure.Kept(dir)
		add(kept)
		ds = append(ds, diag.PluginFailed(failure, kept.Path))
	}
	for _, name := range p.Failed {
		// An agent skipped for an error keeps its installed copy.
		add(artifact.Artifact{Path: filepath.Join(dir, name+".md"), Kept: &artifact.SourceLink{Path: name}})
	}
	for _, a := range p.Agents {
		name := a.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(a.Path), ".md")
		}
		spec := openCodeAgentSpec{
			Name: name, Path: a.Path, Generated: a.Generated, Global: true,
			Marker: source.MachineMarkerSource(a.Path, p.AgentSourceDir, cfg.Machine.AgentMarkerDir),
			Output: filepath.Join(dir, name+".md"), Agents: p.AgentRoster,
		}
		if a.Plugin != nil {
			spec.Marker, spec.PluginRoot = source.PluginMarkerSource(a.Plugin, a.Path), a.Plugin.Root
		}
		renderOpenCodeAgent(spec, a.Front, a.Body, map[string]string{}, cfg.OpenCode.ModelMap, known, cfg.Rewrites, add, problem, warn, cfg)
	}
	return outputs, ds
}

// pluginCommandEntries are a plugin's commands, then its skills as skill
// directory entries, each Rel "{plugin}/{rel}".
func pluginCommandEntries(plugin source.Plugin) []source.Entry {
	entries := append([]source.Entry(nil), plugin.Commands...)
	for _, skill := range plugin.Skills {
		entries = append(entries, source.Entry{Path: filepath.Dir(skill.Path), Rel: skill.Rel, SkillDir: true})
	}
	return entries
}
