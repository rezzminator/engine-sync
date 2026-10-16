package source

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// MachineOptions identifies provenance and every Claude registry receiving agents.
type MachineOptions struct {
	MarkerDir        string
	ClaudeConfigDirs []string
}

// declarationLine finds a top-level from: key in a source whose header does
// not parse.
var declarationLine = regexp.MustCompile(`(?m)^from:`)

// LoadMachine snapshots original roles and declarations before rendering.
func LoadMachine(home, agentsDir, generatedDir string, options ...MachineOptions) (Project, error) {
	opts := MachineOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	if len(opts.ClaudeConfigDirs) == 0 {
		opts.ClaudeConfigDirs = []string{filepath.Join(home, ".claude")}
	}
	for i, dir := range opts.ClaudeConfigDirs {
		if dir == "" {
			opts.ClaudeConfigDirs = slices.Clone(opts.ClaudeConfigDirs)
			opts.ClaudeConfigDirs[i] = filepath.Join(home, ".claude")
		}
	}
	p := Project{Root: agentsDir, Home: home, Scope: MachineScope, AgentSourceDir: agentsDir, GeneratedDir: generatedDir, documents: map[string]document{}, directories: map[string]directory{}}
	collides, err := machineSourceInRegistry(agentsDir, opts.ClaudeConfigDirs)
	if err != nil {
		return p, err
	}
	blocked, brokenSources := map[string]bool{}, map[string]bool{}
	paths, err := filepath.Glob(filepath.Join(agentsDir, "*.md"))
	if err != nil {
		return p, err
	}

	for _, path := range paths {
		if filepath.Base(path) == "README.md" {
			continue
		}
		raw, err := p.Read(path)
		if err != nil {
			if os.IsNotExist(err) {
				p.Warnings = append(p.Warnings, fmt.Sprintf("dangling source %s: %v", path, err))
				continue
			}
			return p, fmt.Errorf("read %s: %w", path, err)
		}
		front, body, err := Parse(string(raw))
		if err != nil {
			// One broken source is skipped and named, its installed copy kept;
			// the others still load.
			p.Problems = append(p.Problems, DescribeParseError(path, err))
			brokenSources[strings.TrimSuffix(filepath.Base(path), ".md")] = true
			p.Failed = append(p.Failed, failedSourceNames(path, raw)...)
			p.Declares = p.Declares || declarationLine.Match(raw)
			continue
		}
		if machineControls(front.Node) {
			p.Declares = true
			if collides {
				name := strings.TrimSuffix(filepath.Base(path), ".md")
				blocked[name] = true
				p.Problems = append(p.Problems, fmt.Sprintf("%s: Claude Code loads it raw from this agents dir; set sourceAgentsDir (or sourceRepo) to a directory outside every Claude agents dir", path))
				p.Failed = append(p.Failed, name)
			}
			continue
		}
		p.Agents = append(p.Agents, Agent{Name: front.Fields["name"], Path: path, Raw: raw, Front: front, Body: body})
	}
	decl, err := readDeclarations(agentsDir, generatedDir, []string{opts.MarkerDir})
	if err != nil {
		return p, err
	}
	for name := range blocked {
		delete(decl.declarations, name)
		delete(decl.failures, name)
	}
	v, failed := decl.renderEach()
	for _, name := range slices.Sorted(maps.Keys(failed)) {
		// A source that did not parse is already named above.
		if brokenSources[name] || slices.Contains(p.Failed, name) {
			continue
		}
		p.Problems = append(p.Problems, declarationProblem(failed[name]))
		// A shared file is never an agent: its failure has no copy to keep.
		if _, shared := decl.shared[name]; !shared && !strings.Contains(name, "/") {
			p.Failed = append(p.Failed, name)
		}
	}

	for _, a := range v {
		front, body, err := Parse(string(a.Content))
		if err != nil {
			p.Problems = append(p.Problems, "VARIANT-INVALID "+DescribeParseError(a.Path, err))
			p.Failed = append(p.Failed, a.Name)
			continue
		}
		p.documents[a.Path] = document{Raw: a.Content, Front: front, Body: body}
		p.Agents = append(p.Agents, Agent{Name: a.Name, Path: a.Path, Raw: a.Content, Front: front, Body: body, Generated: true})
	}
	p.frozen = true
	return p, nil
}

func LoadMachineCommands(home, sourceHome string, uninstall bool, policies ...config.Config) Project {
	p := Project{Root: sourceHome, Home: home, SourceHome: sourceHome, Scope: MachineScope, MachineCommands: true, documents: map[string]document{}, directories: map[string]directory{}}
	p.SourceCommandDir = filepath.Join(sourceHome, ".claude", "commands")
	if len(policies) > 0 && sourceHome == home && policies[0].ClaudeConfigDir != "" {
		p.SourceCommandDir = filepath.Join(policies[0].ClaudeConfigDir, "commands")
	}
	if !uninstall {
		report := Reporter{Problem: func(s string) { p.Problems = append(p.Problems, s) }, Warning: func(s string) { p.Warnings = append(p.Warnings, s) }, Dangling: func(path, rel string, err error) {
			p.Unresolved = append(p.Unresolved, Unresolved{Path: path, Rel: rel, Target: LinkTarget(path), Err: err})
		}}
		dir := p.SourceCommandDir
		for _, e := range p.Markdown(dir, nil, report) {
			path := e.Path
			if e.SkillDir {
				path = filepath.Join(path, "SKILL.md")
			}
			_, _ = p.Read(path)
		}
	}
	p.frozen = true
	return p
}

// GeneratedDirectory is where declarations render: the source repository's
// machine.sourceGeneratedAgentsDir, empty when unset — there is no other
// place, and a declaration then fails by name (see readDeclarations).
func GeneratedDirectory(home, repo string, cfg config.Config) string {
	if cfg.Machine.SourceGeneratedAgentsDir == "" {
		return ""
	}
	return filepath.Join(paths.GlobalSourceRoot(home, repo, cfg.ClaudeConfigDir), filepath.FromSlash(cfg.Machine.SourceGeneratedAgentsDir))
}

func AgentsDirectory(home, repo string, cfg config.Config) string {
	if repo == "" && cfg.Machine.ClaudeAgentsDir != "" {
		if filepath.IsAbs(cfg.Machine.ClaudeAgentsDir) {
			return cfg.Machine.ClaudeAgentsDir
		}
		return filepath.Join(home, cfg.Machine.ClaudeAgentsDir)
	}
	if repo != "" {
		return filepath.Join(repo, filepath.FromSlash(cfg.Machine.SourceAgentsDir))
	}
	return paths.GlobalAgentsSource(home, repo, cfg.ClaudeConfigDir)
}

// MachineMarkerSource names what a machine-global agent's generated marker
// credits: the clone path of an original agent, or its declaration for a
// generated agent, whose Markdown only ever existed in the engine-sync-owned
// generated directory. It is deliberately clone-relative — a host-absolute
// path in a marker says nothing an operator on another machine can act on.
// markerDir is machine.agentMarkerDir; empty is the agents directory's base.
func MachineMarkerSource(mdPath, agentsDir, markerDir string) string {
	relDir := filepath.Base(agentsDir)
	if markerDir != "" {
		relDir = markerDir
	}
	relDir = filepath.ToSlash(relDir) + "/"
	return relDir + filepath.Base(mdPath)
}

// Directory identity follows aliases, rather than comparing spelling alone.
func machineSourceInRegistry(agentsDir string, configDirs []string) (bool, error) {
	source, err := os.Stat(agentsDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect machine agents source %s: %w", agentsDir, err)
	}
	for _, dir := range configDirs {
		registry := filepath.Join(dir, "agents")
		dest, err := os.Stat(registry)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("inspect Claude agents dir %s: %w", registry, err)
		}
		if os.SameFile(source, dest) {
			return true, nil
		}
	}
	return false, nil
}
