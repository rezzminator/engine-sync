package codex

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/paths"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"strings"
)

func machineRoots(p source.Project, cfg config.Config) []artifact.Root {
	var roots []artifact.Root
	if p.MachineCommands {
		for _, home := range p.CodexHomes {
			for _, name := range []string{"agents", "config.toml"} {
				roots = append(roots, artifact.Root{Path: filepath.Join(home, name), ValidateOnly: true})
			}
			for _, kind := range []string{"skills", "prompts"} {
				roots = append(roots, artifact.Root{Path: filepath.Join(home, kind), SourceRoots: []string{filepath.Join(p.SourceHome, ".claude", "commands"), filepath.Join(p.Home, ".claude", "commands")}})
			}
		}
		return roots
	}
	roots = append(roots, artifact.Root{Path: filepath.Join(p.Home, cfg.Machine.GeneratedAgentsDir), Boundary: p.Home, Suffix: ".md", SourceRoots: []string{p.AgentSourceDir}})
	sources := []string{p.AgentSourceDir, filepath.Join(p.Home, cfg.Machine.GeneratedAgentsDir), filepath.Join(p.Home, cfg.Machine.LegacyCodexAgentsDir), p.Root}
	for _, home := range p.CodexHomes {
		roots = append(roots, artifact.Root{Path: filepath.Join(home, "agents"), SourceRoots: sources, InspectForeign: true})
		roots = append(roots, artifact.Root{Path: filepath.Join(home, "config.toml"), ValidateOnly: true})
	}
	// Each Claude registry link is its own root, so its claim test keeps these
	// sources with no Codex home, without walking the rest of the registry.
	for _, a := range p.Agents {
		for _, dir := range p.ClaudeConfigDirs {
			path := filepath.Join(dir, "agents", filepath.Base(a.Path))
			roots = append(roots, artifact.Root{Path: path, SourceRoots: sources, ValidateOnly: true})
		}
	}

	return roots
}

// claudeRegistryLinks names the link each Claude config directory owes agent.
func claudeRegistryLinks(p source.Project, agent string) ([]string, error) {
	var links []string
	for _, dir := range p.ClaudeConfigDirs {
		path := filepath.Join(dir, "agents", filepath.Base(agent))
		same, e := paths.SameEntry(path, agent)
		if e != nil {
			return nil, e
		}
		if !same {
			links = append(links, path)
		}
	}
	return links, nil
}
func renderCodexMachine(p source.Project, cfg config.Config) ([]artifact.Artifact, []diag.Diagnostic) {
	var outputs []artifact.Artifact
	var diagnostics []diag.Diagnostic
	if p.MachineCommands {
		result := Result{Source: &p}
		add := func(a artifact.Artifact) { outputs = append(outputs, codexArtifact(a)) }
		problem := func(s string) { diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Error, Message: s}) }
		warn := func(s string) { diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, Message: s}) }
		for _, s := range p.Problems {
			problem(s)
		}
		for _, s := range p.Warnings {
			warn(s)
		}
		for _, home := range p.CodexHomes {
			first := len(outputs)
			compileInstalledGlobalCommands(p.SourceHome, filepath.Dir(home), add, problem, warn, &result, cfg) // destinations are retargeted below, independent of home basename
			for i := first; i < len(outputs); i++ {
				base := filepath.Join(filepath.Dir(home), ".codex")
				if rel, e := filepath.Rel(base, outputs[i].Path); e == nil && !strings.HasPrefix(rel, "..") {
					outputs[i].Path = filepath.Join(home, rel)
				}
			}
		}
		for _, s := range result.Dangling {
			found := false
			for i := range diagnostics {
				if diagnostics[i].Message == s {
					diagnostics[i].State = reconciler.Dangling
					diagnostics[i].Remedy = "retire the stale link; `engine-sync global` prunes an orphaned global-command link automatically"
					found = true
				}
			}
			if !found {
				diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, State: reconciler.Dangling, Message: s, Remedy: "retire the stale link; `engine-sync global` prunes an orphaned global-command link automatically"})
			}
		}

		return outputs, diagnostics
	}

	for _, s := range p.Problems {
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Error, Message: s})
	}
	for _, s := range p.Warnings {
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, Message: s})
	}
	for _, a := range p.Agents {
		if alias := a.Front.Fields["model"]; len(p.CodexHomes) > 0 && alias != "" && alias != "inherit" && cfg.Machine.ModelMap[alias] == "" && a.Front.Fields["codex-model"] == "" {
			diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, Path: a.Path, Message: fmt.Sprintf("unmapped Codex model alias %q in %s — model omitted", alias, a.Path)})
		}
		name, text, err := renderGlobalAgentTOML(a.Path, string(a.Raw), p.AgentSourceDir, cfg)
		if err != nil {
			diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Error, Path: a.Path, Message: err.Error()})
			continue
		}
		for _, home := range p.CodexHomes {
			outputs = append(outputs, artifact.Artifact{Path: filepath.Join(home, "agents", name+".toml"), Content: []byte(text), Source: a.Path, Syntax: artifact.TOML})
		}
		if a.Generated {
			outputs = append(outputs, artifact.Artifact{Path: a.Path, Content: a.Raw, Source: filepath.Join(p.AgentSourceDir, "variants.json"), Syntax: artifact.YAMLFront})
		}
		links, e := claudeRegistryLinks(p, a.Path)
		if e != nil {
			diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Error, Path: a.Path, Message: e.Error()})
			continue
		}
		for _, path := range links {
			outputs = append(outputs, artifact.Artifact{Path: path, Kind: artifact.Link, Target: a.Path, Source: a.Path})
		}
	}
	return outputs, diagnostics
}
