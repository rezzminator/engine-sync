package codex

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/engine"
	"github.com/rezzminator/engine-sync/internal/paths"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"os"
	"path/filepath"
	"strings"
)

// Mode selects whether the reconciler may change the filesystem.
type Mode uint8

const (
	ModeBuild Mode = iota
	ModeCheck
	frontmatterFence = "---"
)

// Options is deliberately filesystem-only: the compiler has no process,
// network, or live-editor dependencies.
type Options struct {
	Root         string
	Home         string
	Mode         Mode
	CLIOverrides CLIOverrides
	// Capabilities are the --capability values; the run's capability set
	// adds the targets.json list (config.Capabilities).
	Capabilities []string
	Config       *config.Config
	Source       *claudesource.Project
}

type Result = diag.Report

// Action is one filesystem mutation a build performs. Check returns the same
// list without writing, while Problems retains its existing drift diagnostics.
type Action = reconciler.Action

// GlobalCommandsOptions selects the host HOME whose installed Claude
// commands are mirrored into the global Codex prompt and skill registries.
// Unlike Options, it deliberately has no repository root: host install and
// uninstall must not compile or reconcile project files as a side effect.
type GlobalCommandsOptions struct {
	Config     *config.Config
	CodexHomes []string
	Uninstall  bool
	Home       string
	SourceHome string
	Mode       Mode
}

// Run discovers the incumbent Claude source tree, transforms it into Codex
// artifacts, and reconciles only outputs carrying this compiler's marker.
// Build writes each file atomically; check computes the same plan and writes
// nothing.
func RunProject(options Options) (Result, error) {
	root, err := resolveRoot(options.Root)
	if err != nil {
		return Result{}, err
	}
	home, err := resolveCodexHome(options.Home)
	if err != nil {
		return Result{}, err
	}
	policy := options.Config
	if policy == nil {
		resolved, err := config.LoadConfigLayers(root, home)
		if err != nil {
			return Result{}, err
		}
		policy = &resolved
	}
	resolved, err := policy.WithCodexOverrides(options.CLIOverrides)
	if err != nil {
		return Result{}, err
	}
	return executeCodex(root, home, resolved, options.Mode, options.Source)
}

func executeCodex(root, home string, policy config.Config, mode Mode, model *claudesource.Project) (Result, error) {
	if model == nil {
		p := claudesource.LoadProject(root, home, policy)
		model = &p
	}
	target := Target{}
	r, ok := engine.Execute(*model, policy, codexMode(mode), target.Render, target.Roots, target.Validate)
	if !model.Workbench && (policy).Codex.GlobalCommands {
		machine, e := config.Resolve("", home, paths.Getenv)
		if e != nil {
			return Result{}, e
		}
		roster, e := config.ResolveTargets(paths.Getenv, home, machine, config.Targets{})
		if e != nil {
			return Result{}, e
		}
		globals, pass := engine.Commands(home, "", machine, *roster.CodexHomes, false, codexMode(mode), target.Render, target.Roots, target.Validate)
		ok = ok && pass
		r.Problems = append(r.Problems, globals.Problems...)
		r.Warnings = append(r.Warnings, globals.Warnings...)
		r.Actions = append(r.Actions, globals.Actions...)
		r.Wrote += globals.Wrote
		r.Unchanged += globals.Unchanged
		r.Deleted += globals.Deleted
	}

	return Result{OK: ok, Dangling: r.Dangling, Warnings: r.Warnings, Problems: r.Problems, Rebuildable: r.Rebuildable, Wrote: r.Wrote, Unchanged: r.Unchanged, Deleted: r.Deleted, Actions: r.Actions}, nil
}

// RunGlobalCommands compiles and reconciles only $HOME/.claude/commands into
// $HOME/.codex/{prompts,skills}. Check is read-only; build replaces or deletes
// only artifacts carrying the compiler marker and preserves every unmarked
// conflict.
func RunGlobalCommands(options GlobalCommandsOptions) (Result, error) {
	home, err := resolveCodexHome(options.Home)
	if err != nil {
		return Result{}, err
	}
	sourceHome := home
	if options.SourceHome != "" {
		sourceHome, err = resolveCodexHome(options.SourceHome)
		if err != nil {
			return Result{}, err
		}
	}
	cfg, err := config.Resolve("", home, paths.Getenv)
	if err != nil {
		return Result{}, err
	}
	if options.Config != nil {
		cfg = *options.Config
	}
	p := claudesource.LoadMachineCommands(home, sourceHome, options.Uninstall, cfg)
	p.CodexHomes = options.CodexHomes
	if p.CodexHomes == nil {
		roster, err := config.ResolveTargets(paths.Getenv, home, cfg, config.Targets{})
		if err != nil {
			return Result{}, err
		}
		p.CodexHomes = *roster.CodexHomes
	}
	t := Target{}
	r, ok := engine.Execute(p, cfg, codexMode(options.Mode), t.Render, t.Roots, t.Validate)
	return Result{OK: ok, Warnings: r.Warnings, Problems: r.Problems, Rebuildable: r.Rebuildable, Dangling: r.Dangling, Wrote: r.Wrote, Unchanged: r.Unchanged, Deleted: r.Deleted, Actions: r.Actions}, nil

}

// Build and Check are explicit API entry points for callers that do not want
// to construct a Mode value.  Both retain the same deterministic plan.
func Build(options Options) (Result, error) {
	options.Mode = ModeBuild
	return RunProject(options)
}

func CheckProject(options Options) (Result, error) {
	options.Mode = ModeCheck
	return RunProject(options)
}

func resolveRoot(root string) (string, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve repository root: %w", err)
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	return paths.PhysicalPath(root)
}

func resolveCodexHome(home string) (string, error) {
	if home == "" {
		var err error
		home, err = paths.Home()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Clean(home), nil
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

type Target struct{}

func (Target) Name() string { return "codex" }

func (Target) Roots(p claudesource.Project, cfg config.Config) []artifact.Root {
	if p.Scope == claudesource.MachineScope {
		return machineRoots(p, cfg)
	}
	sources := []string{filepath.Join(p.Root, ".claude"), filepath.Join(p.Home, ".claude")}
	for _, child := range p.Children {
		sources = append(sources, filepath.Join(child.Root, ".claude"))
	}
	names := []string{filepath.Join(p.Root, ".codex", "agents"), filepath.Join(p.Root, ".codex", "skills")}
	roots := make([]artifact.Root, 0, len(names))
	for _, project := range p.Projects {
		roots = append(roots, artifact.Root{Path: filepath.Join(p.Root, project, "AGENTS.md"), ValidateOnly: true})
	}
	for _, name := range names {
		roots = append(roots, artifact.Root{Path: name, SourceRoots: sources, InspectForeign: filepath.Base(name) == "agents"})
	}
	for _, name := range []string{filepath.Join(p.Root, ".codex", "config.toml")} {
		roots = append(roots, artifact.Root{Path: name, ValidateOnly: true})
	}
	if !p.Workbench && !cfg.Codex.GlobalCommands {
		homes := p.CodexHomes
		if homes == nil {
			homes = []string{filepath.Join(p.Home, ".codex")}
		}
		for _, home := range homes {
			for _, name := range []string{"agents", "config.toml"} {
				roots = append(roots, artifact.Root{Path: filepath.Join(home, name), ValidateOnly: true})
			}
		}
	}
	return roots
}

func (Target) Render(p claudesource.Project, cfgAll config.Config) ([]artifact.Artifact, []diag.Diagnostic) {
	if p.Scope == claudesource.MachineScope {
		return renderCodexMachine(p, cfgAll)
	}
	root, cfg := p.Root, cfgAll.Codex
	if p.Workbench {
		cfg.GlobalCommands = false
	}
	result := Result{Warnings: []string{}, Problems: []string{}, Dangling: []string{}, Source: &p}
	outputs := make([]artifact.Artifact, 0)
	add := func(output artifact.Artifact) { outputs = append(outputs, output) }
	warn := func(message string) { result.Warnings = append(result.Warnings, message) }
	problem := func(message string) { result.Problems = append(result.Problems, message) }

	projects := discoverProjects(root, cfg, &result, cfgAll.WorkbenchMarker)
	roster := discoverCommandRoster(root, cfg, &result)
	for _, rel := range cfgAll.CommandDirs {
		for name, value := range discoverCommandRosterIn(filepath.Join(root, rel), cfg.ExcludeDirs, &result) {
			roster[name] = value
		}
	}
	if p.SourceCommandDir != "" {
		for name, value := range discoverCommandRosterIn(p.SourceCommandDir, nil, &result) {
			roster[name] = value
		}
	}
	transform := TransformOptions{Review: cfg.Review, ModelMap: cfg.ModelMap, Commands: roster, Source: result.Source, Rewrites: cfgAll.Rewrites}
	agentsDocumentTransform := transform
	agentsDocumentTransform.ReplaceClaudeFile = true

	for _, project := range projects {
		src := filepath.Join(root, project, "CLAUDE.md")
		content, ok := compileSourceMarkdown(src, agentsDocumentTransform, problem)
		if !ok {
			continue
		}
		if project == "." {
			adapter, err := config.RenderTemplate(cfg.RootAdapter, config.TemplateData{Engine: "Codex", Source: "CLAUDE.md", Root: "AGENTS.md", Models: cfg.ModelMap})
			if err != nil {
				problem(fmt.Sprintf("render codex.rootAdapter for %s: %v", src, err))
				continue
			}
			content += adapter
		}
		rel, _ := filepath.Rel(root, src)
		add(
			artifact.Artifact{
				Path:    filepath.Join(root, project, "AGENTS.md"),
				Content: []byte(generatedHeader(filepath.ToSlash(rel)) + "\n" + content), Source: src, Syntax: artifact.Markdown,
			},
		)
	}

	compileAgents(root, projects, cfgAll, transform, add, problem, warn, &result)
	compileRepoCommands(root, cfg, transform, roster, add, problem, warn, &result)
	compileRepoSkills(root, add, problem, &result)

	mcp, err := renderMCP(root, &p, "")
	if err != nil {
		problem(err.Error())
	} else {
		result.Warnings = append(result.Warnings, mcp.Notes...)
		output := artifact.Artifact{Path: filepath.Join(root, ".codex", "config.toml"), Content: []byte(mcp.Content), Kind: artifact.Region, Source: filepath.Join(root, ".mcp.json"), Syntax: artifact.TOML, OptionalEmpty: true}
		output.Merge = func(current []byte) ([]byte, []string, []string) {
			merged, err := renderMCP(root, &p, string(current))
			if err != nil {
				return nil, nil, []string{err.Error()}
			}
			if len(merged.Problems) == 0 && merged.Content != "" {
				if err := validateTOML(merged.Content); err != nil {
					merged.Problems = append(merged.Problems, fmt.Sprintf("%s generated %s: %v", reconciler.Unparseable, output.Path, err))
				}
			}
			return []byte(merged.Content), nil, merged.Problems
		}
		add(output)
	}
	artifacts := make([]artifact.Artifact, 0, len(outputs))
	for _, output := range outputs {
		artifacts = append(artifacts, codexArtifact(output))
	}
	var diagnostics []diag.Diagnostic
	dangling := map[string]bool{}
	for _, message := range result.Dangling {
		dangling[message] = true
	}
	for _, message := range result.Warnings {
		state := reconciler.State("")
		if dangling[message] {
			state = reconciler.Dangling
		}
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, State: state, Message: message, Remedy: "retire the stale link; `engine-sync global` prunes an orphaned global-command link automatically"})
	}
	for _, message := range result.Problems {
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Error, Message: message})
	}
	return artifacts, diagnostics
}

func (Target) Validate(artifacts []artifact.Artifact) []diag.Diagnostic {
	var diagnostics []diag.Diagnostic
	for _, a := range artifacts {
		if a.Kept != nil || a.Kind == artifact.Link || !strings.HasSuffix(a.Path, ".toml") {
			continue
		}
		if err := validateTOML(string(a.Content)); err != nil {
			diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Finding, State: reconciler.Unparseable, Path: a.Path, Message: fmt.Sprintf("%s %s: %v", reconciler.Unparseable, a.Path, err)})
		}
	}
	return diagnostics
}
