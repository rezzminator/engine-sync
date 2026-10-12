package opencode

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/engine"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
)

type Mode uint8

const (
	ModeBuild Mode = iota
	ModeCheck
	ModeDoctor
)

type Options struct {
	Root   string
	Home   string
	Mode   Mode
	Config *config.Config
	Source *claudesource.Project
}

type Result = diag.Report

type Action = reconcile.Action

func compileOpenCode(options Options) (Result, error) {
	p := options.Source
	if p == nil {
		loaded := claudesource.LoadProject(options.Root, options.Home, *options.Config)
		p = &loaded
	}
	target := Target{}
	r, ok := engine.Execute(*p, *options.Config, openCodeMode(options.Mode), target.Render, target.Roots, target.Validate)
	if !p.Workbench && (*options.Config).Codex.GlobalCommands {
		machine, e := config.Resolve("", options.Home, paths.Getenv)
		if e != nil {
			return Result{}, e
		}
		roster, e := config.ResolveTargets(paths.Getenv, options.Home, machine, config.Targets{})
		if e != nil {
			return Result{}, e
		}
		globals, pass := engine.Commands(options.Home, "", machine, *roster.CodexHomes, false, openCodeMode(options.Mode), target.Render, target.Roots, target.Validate)
		ok = ok && pass
		r.Problems = append(r.Problems, globals.Problems...)
		r.Warnings = append(r.Warnings, globals.Warnings...)
		r.Actions = append(r.Actions, globals.Actions...)
		r.Wrote += globals.Wrote
		r.Unchanged += globals.Unchanged
		r.Deleted += globals.Deleted
	}

	return Result{OK: ok, Warnings: r.Warnings, Problems: r.Problems, Rebuildable: r.Rebuildable, Wrote: r.Wrote, Unchanged: r.Unchanged, Deleted: r.Deleted, Actions: r.Actions}, nil
}

func Compile(options Options) (Result, error) {
	root, err := resolveOpenCodePath(options.Root, "repository root")
	if err != nil {
		return Result{}, err
	}
	home, err := resolveOpenCodePath(options.Home, "home")
	if err != nil {
		return Result{}, err
	}
	cfg := options.Config
	if cfg == nil {
		resolved, err := config.LoadConfigLayers(root, home)
		if err != nil {
			return Result{}, err
		}
		cfg = &resolved
	}

	options.Root, options.Home, options.Config = root, home, cfg
	return compileOpenCode(options)
}

func resolveOpenCodePath(path, label string) (string, error) {
	if path == "" {
		var err error
		if label == "home" {
			path, err = paths.Home()
		} else {
			path, err = filepath.Abs(".")
		}
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", label, err)
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s %q: %w", label, path, err)
	}
	return paths.PhysicalPath(abs)
}

func openCodeName() string {
	return engine.MustLookup(engine.OpenCode).LongName
}

// GeneratedMarkers are the lines that mark a file this compiler (or its
// declared predecessor) wrote: a project whose outputs carry one is in
// engine-sync's scope.
func OpenCodeMarkers() []string { return artifact.Markers() }

type Target struct{}

func (Target) Name() string { return "opencode" }

func (Target) Roots(p claudesource.Project, cfg config.Config) []artifact.Root {
	if p.Scope == claudesource.MachineScope && p.MachineCommands {
		return []artifact.Root{{Path: filepath.Join(p.Home, ".config", openCodeName(), "command"), SourceRoots: []string{p.SourceCommandDir}}}
	}
	if p.Scope == claudesource.MachineScope {
		sources := []string{p.AgentSourceDir, filepath.Join(p.Home, cfg.Machine.GeneratedAgentsDir), p.Root}
		return []artifact.Root{{Path: MachineAgentsDir(p.Home), SourceRoots: sources, InspectForeign: true}}
	}
	sources := []string{filepath.Join(p.Root, ".claude"), filepath.Join(p.Home, ".claude")}
	for _, child := range p.Children {
		sources = append(sources, filepath.Join(child.Root, ".claude"))
	}
	names := []string{filepath.Join(p.Root, ".opencode", "agent"), filepath.Join(p.Root, ".opencode", "command"), filepath.Join(p.Root, ".opencode", "skills")}

	var roots []artifact.Root
	roots = append(roots, artifact.Root{Path: filepath.Join(p.Root, ".opencode", "opencode.jsonc"), ValidateOnly: true})
	for _, name := range cfg.OpenCode.MirrorFiles {
		roots = append(roots, artifact.Root{Path: filepath.Join(p.Root, ".opencode", name), ValidateOnly: true})
	}
	for _, name := range names {
		roots = append(roots, artifact.Root{Path: name, SourceRoots: sources})
	}
	return roots
}

func (Target) Render(p claudesource.Project, cfg config.Config) ([]artifact.Artifact, []diag.Diagnostic) {
	if p.Scope == claudesource.MachineScope && p.MachineCommands {
		return renderOpenCodeMachine(p, cfg)
	}
	if p.Scope == claudesource.MachineScope {
		return renderOpenCodeMachineAgents(p, cfg)
	}
	root, model := p.Root, &p
	var diagnostics []diag.Diagnostic
	var outputs []artifact.Artifact
	add := func(a artifact.Artifact) { outputs = append(outputs, a) }
	problem := func(format string, args ...any) {
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Error, Message: fmt.Sprintf(format, args...)})
	}
	warn := func(format string, args ...any) {
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, Message: fmt.Sprintf(format, args...)})
	}
	skip := func(format string, args ...any) {
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, Message: fmt.Sprintf(format, args...)})
	}
	seen := map[string]bool{}
	dangling := func(path string, err error) {
		if seen[path] {
			return
		}
		seen[path] = true
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, State: reconcile.Dangling, Path: path, Message: fmt.Sprintf("%s %s: %v", reconcile.Dangling, path, err)})
	}
	roster := map[string]string{}
	dirs := []string{filepath.Join(root, ".claude", "commands")}
	for _, rel := range cfg.CommandDirs {
		dirs = append(dirs, filepath.Join(root, rel))
	}
	for _, dir := range dirs {
		discoverOpenCodeCommandRoster(dir, roster, problem, dangling, model)
	}

	if p.SourceCommandDir != "" {
		discoverOpenCodeCommandRoster(p.SourceCommandDir, roster, problem, dangling, model)
	}
	projects := p.Projects
	for _, message := range p.Problems {
		problem("%s", message)
	}
	modelMap := cfg.OpenCode.ModelMap
	compileOpenCodeAgents(root, projects, roster, modelMap, add, problem, skip, warn, dangling, model, cfg)
	compileOpenCodeCommands(
		filepath.Join(root, ".claude", "commands"),
		".claude/commands",
		roster,
		filepath.Join(root, ".opencode", "command"),
		add,
		problem,
		skip,
		dangling, model, cfg,
	)

	compileOpenCodeSkills(root, add, problem, skip, dangling, model)
	compileConfigWithSource(model, root, add, problem, warn, cfg.OpenCode)
	compileOpenCodeMirrors(root, model, cfg, add, problem, warn)

	artifacts := make([]artifact.Artifact, 0, len(outputs))
	for _, output := range outputs {
		artifacts = append(artifacts, openCodeArtifact(output))
	}
	return artifacts, diagnostics
}
