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
	return filepath.Clean(abs), nil
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
	sources := []string{filepath.Join(p.Root, ".claude"), filepath.Join(p.Home, ".claude")}
	for _, child := range p.Children {
		sources = append(sources, filepath.Join(child.Root, ".claude"))
	}
	names := []string{filepath.Join(p.Root, ".opencode", "agent"), filepath.Join(p.Root, ".opencode", "command"), filepath.Join(p.Root, ".opencode", "skills")}
	if !p.Workbench {
		names = append(names, filepath.Join(p.Home, ".config", openCodeName(), "command"))
	}
	var roots []artifact.Root
	for _, name := range names {
		roots = append(roots, artifact.Root{Path: name, SourceRoots: sources})
	}
	return roots
}

func (Target) Render(p claudesource.Project, cfg config.Config) ([]artifact.Artifact, []diag.Diagnostic) {
	root, home, model := p.Root, p.Home, &p
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
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, State: reconcile.Invalid, Message: fmt.Sprintf(format, args...)})
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
	for _, source := range []string{
		filepath.Join(root, ".claude", "commands"),
		filepath.Join(root, "templates", "global", "commands"),
		filepath.Join(home, ".claude", "commands"),
	} {
		discoverOpenCodeCommandRoster(source, roster, problem, dangling, model)
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
	if !p.Workbench {
		compileOpenCodeCommands(
			filepath.Join(home, ".claude", "commands"),
			"$HOME/.claude/commands",
			roster,
			filepath.Join(home, ".config", openCodeName(), "command"),
			add,
			problem,
			skip,
			dangling, model, cfg,
		)
	}
	compileOpenCodeSkills(root, add, problem, skip, dangling, model)
	compileConfigWithSource(model, root, add, problem, warn, cfg.OpenCode)
	compileOpenCodeMirrors(root, model, cfg, add, problem, warn)

	artifacts := make([]artifact.Artifact, 0, len(outputs))
	for _, output := range outputs {
		artifacts = append(artifacts, openCodeArtifact(output))
	}
	return artifacts, diagnostics
}
