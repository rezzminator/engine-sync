package source

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"os"
	"path/filepath"
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
			return p, fmt.Errorf("parse %s: %w", path, err)
		}
		p.Agents = append(p.Agents, Agent{Name: front.Fields["name"], Path: path, Raw: raw, Front: front, Body: body})
	}
	v, err := LoadGlobalAgentVariants(agentsDir, generatedDir, variants...)
	if err != nil {
		return p, err
	}
	for _, a := range v {
		front, body, err := Parse(string(a.Content))
		if err != nil {
			return p, err
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
