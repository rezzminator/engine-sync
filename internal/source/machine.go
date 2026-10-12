package source

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// LoadMachine snapshots original roles and declared variants before rendering.
func LoadMachine(home, agentsDir, generatedDir string, variants ...string) (Project, error) {
	p := Project{Root: agentsDir, Home: home, Scope: MachineScope, AgentSourceDir: agentsDir, documents: map[string]document{}, directories: map[string]directory{}}
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
			p.Failed = append(p.Failed, failedSourceNames(path, raw)...)
			continue
		}
		p.Agents = append(p.Agents, Agent{Name: front.Fields["name"], Path: path, Raw: raw, Front: front, Body: body})
	}
	declPath, _ := variantsPath(agentsDir, variants)
	decl, err := readVariantsDecl(agentsDir, generatedDir, variants)
	var v []GlobalAgentVariant
	if err != nil {
		// The declaration fails as a whole: every variant is skipped under
		// this one error and keeps its installed copy; the originals load.
		p.Problems = append(p.Problems, fmt.Sprintf("VARIANTS-INVALID %s — %v", declPath, err))
		p.Failed = append(p.Failed, p.generatedNames(generatedDir)...)
	} else if decl != nil {
		var failed map[string]error
		v, failed = decl.renderEach()
		for _, name := range slices.Sorted(maps.Keys(failed)) {
			p.Problems = append(p.Problems, variantProblem(failed[name]))
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
// credits: the clone path of an original agent, or variants.json for a
// variant, whose Markdown only ever existed in the engine-sync-owned
// generated directory. It is deliberately clone-relative — a host-absolute
// path in a marker says nothing an operator on another machine can act on.
// markerDir is machine.agentMarkerDir; empty is the agents directory's base.
func MachineMarkerSource(mdPath, agentsDir, markerDir string) string {
	relDir := filepath.Base(agentsDir)
	if markerDir != "" {
		relDir = markerDir
	}
	relDir = filepath.ToSlash(relDir) + "/"
	if filepath.Dir(mdPath) == filepath.Clean(agentsDir) {
		return relDir + filepath.Base(mdPath)
	}
	return relDir + "variants.json"
}
