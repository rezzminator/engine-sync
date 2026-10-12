package source

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"os"
	"path/filepath"
	"strings"
)

type Scope uint8

const (
	ProjectScope Scope = iota
	MachineScope
)

type Orientation struct{ Path, Dir, Body string }
type Agent struct {
	Name, Path string
	Raw        []byte
	Generated  bool
	Front      Frontmatter
	Body       string
}
type Command struct {
	Name, Path string
	Front      Frontmatter
	Body       string
	Directory  bool
}
type Skill struct{ Name, Path string }
type Unresolved struct {
	Path, Rel, Target string
	Err               error
}
type document struct {
	Raw           []byte
	Front         Frontmatter
	Body          string
	Err, ParseErr error
}
type directory struct {
	Entries            []Entry
	Problems, Warnings []string
	Unresolved         []Unresolved
}
type Project struct {
	SourceHome                   string
	SourceCommandDir             string
	MachineCommands              bool
	AgentSourceDir               string
	CodexHomes, ClaudeConfigDirs []string
	Home                         string
	Workbench                    bool
	frozen                       bool
	mcpPath                      string
	mcpPresent                   bool
	mcpErr                       error
	Root                         string
	Scope                        Scope
	Orientation                  []Orientation
	Agents                       []Agent
	Commands                     []Command
	Skills                       []Skill
	MCP                          []MCPServer
	Children                     []Project
	Unresolved                   []Unresolved
	Projects                     []string
	Problems, Warnings           []string
	// Failed names each machine agent skipped for an error: its installed
	// copies are kept, never retired, until it builds again.
	Failed      []string
	documents   map[string]document
	directories map[string]directory
}

// Load discovers source roots once. Targets select their output policy from this model.
func LoadProject(root, home string, cfg config.Config) Project {
	p := Project{Root: root, Home: home, Scope: ProjectScope, documents: map[string]document{}, directories: map[string]directory{}}
	var workbenchErr error
	p.Workbench, workbenchErr = paths.HasWorkbenchManifest(root, cfg.WorkbenchMarker)
	if workbenchErr != nil {
		p.Problems = append(p.Problems, workbenchErr.Error())
	}
	report := Reporter{Problem: func(s string) { p.Problems = append(p.Problems, s) }, Warning: func(s string) { p.Warnings = append(p.Warnings, s) }, Dangling: func(path, rel string, err error) {
		p.Unresolved = append(p.Unresolved, Unresolved{Path: path, Rel: rel, Target: LinkTarget(path), Err: err})
	}}
	p.Projects = Projects(root, cfg.Codex.Projects, cfg.Codex.ExcludeProjects, report, cfg.WorkbenchMarker)
	for _, relative := range p.Projects {
		child := filepath.Join(root, relative)
		path := filepath.Join(child, "CLAUDE.md")
		raw, err := p.Read(path)
		if err == nil {
			p.Orientation = append(p.Orientation, Orientation{Path: path, Dir: relative, Body: string(raw)})
		}
		agentDir := filepath.Join(child, ".claude", "agents")
		for _, e := range p.Markdown(agentDir, nil, report) {
			if e.SkillDir {
				continue
			}
			if front, body, ok := p.parsed(e.Path, report); ok {
				p.Agents = append(p.Agents, Agent{Name: strings.TrimSuffix(filepath.Base(e.Path), ".md"), Path: e.Path, Front: front, Body: body})
			}
		}
		if relative != "." {
			p.Children = append(p.Children, Project{Root: child, Scope: ProjectScope})
		}
	}
	claudeDir := cfg.ClaudeConfigDir
	if claudeDir == "" {
		claudeDir = filepath.Join(home, ".claude")
	}
	p.SourceCommandDir = filepath.Join(claudeDir, "commands")
	if cfg.Codex.GlobalCommands && !p.Workbench {
		p.Markdown(p.SourceCommandDir, nil, report)
	}
	dirs := []string{filepath.Join(root, ".claude", "commands")}
	for _, rel := range cfg.CommandDirs {
		dirs = append(dirs, filepath.Join(root, filepath.FromSlash(rel)))
	}
	for _, dir := range dirs {
		for _, e := range p.Markdown(dir, cfg.Codex.ExcludeDirs, report) {
			path := e.Path
			if e.SkillDir {
				path = filepath.Join(path, "SKILL.md")
			}
			if front, body, ok := p.parsed(path, report); ok {
				p.Commands = append(p.Commands, Command{Name: strings.ReplaceAll(strings.TrimSuffix(filepath.ToSlash(e.Rel), ".md"), "/", ":"), Path: path, Front: front, Body: body, Directory: e.SkillDir})
			}
		}
	}
	_, _, _ = p.Servers(filepath.Join(root, ".mcp.json"))
	for _, name := range cfg.OpenCode.MirrorFiles {
		_, _ = p.Read(filepath.Join(root, name))
	}
	for _, e := range p.SkillsIn(filepath.Join(root, ".claude", "skills"), report) {
		if e.Target != "" {
			continue
		}
		p.Skills = append(p.Skills, Skill{Name: e.Rel, Path: e.Path})
	}
	p.frozen = true
	return p
}

// Read snapshots source content, including named read failures.
func (p *Project) Read(path string) ([]byte, error) {
	if p == nil {
		return paths.ReadRegular(path)
	}
	if doc, ok := p.documents[path]; ok {
		return doc.Raw, doc.Err
	}
	if p.frozen {
		return nil, os.ErrNotExist
	}
	raw, err := paths.ReadRegular(path)
	front, body, parseErr := Parse(string(raw))
	if p.documents == nil {
		p.documents = map[string]document{}
	}
	p.documents[path] = document{Raw: raw, Front: front, Body: body, Err: err, ParseErr: parseErr}
	return raw, err
}

// parsed is one source file's header for the loader: a header that does not
// parse is reported FRONTMATTER-INVALID, never dropped silently; a read
// failure is the reading target's to name.
func (p *Project) parsed(path string, report Reporter) (Frontmatter, string, bool) {
	front, body, err := p.Frontmatter(path)
	if doc, read := p.documents[path]; err != nil && read && doc.Err == nil {
		report.Problem(DescribeParseError(path, err))
	}
	return front, body, err == nil
}
func (p *Project) Frontmatter(path string) (Frontmatter, string, error) {
	if p == nil {
		raw, err := paths.ReadRegular(path)
		if err != nil {
			return Frontmatter{}, "", err
		}
		return Parse(string(raw))
	}
	if _, err := p.Read(path); err != nil {
		return Frontmatter{}, "", err
	}
	doc := p.documents[path]
	return doc.Front, doc.Body, doc.ParseErr
}
func (p *Project) Markdown(dir string, excludes []string, report Reporter) []Entry {
	if p == nil {
		return Markdown(dir, excludes, report)
	}
	cached, ok := p.directories[dir]
	if !ok && p.frozen {
		return nil
	}
	if !ok {
		collect := Reporter{Problem: func(s string) { cached.Problems = append(cached.Problems, s) }, Warning: func(s string) { cached.Warnings = append(cached.Warnings, s) }, Dangling: func(path, rel string, err error) {
			cached.Unresolved = append(cached.Unresolved, Unresolved{Path: path, Rel: rel, Target: LinkTarget(path), Err: err})
		}}
		cached.Entries = Markdown(dir, excludes, collect)
		p.directories[dir] = cached
	}
	for _, s := range cached.Problems {
		report.Problem(s)
	}
	for _, s := range cached.Warnings {
		report.Warning(s)
	}
	for _, u := range cached.Unresolved {
		if !Excluded(excludes, u.Rel, filepath.Base(u.Path)) {
			report.Dangling(u.Path, u.Rel, u.Err)
		}
	}
	var entries []Entry
	for _, e := range cached.Entries {
		if !Excluded(excludes, e.Rel, filepath.Base(e.Path)) {
			entries = append(entries, e)
		}
	}
	return entries
}

func SkillDirectories(dir string, report Reporter) []Entry {
	items, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		report.Problem("read " + dir + ": " + err.Error())
		return nil
	}
	var entries []Entry
	for _, item := range items {
		path := filepath.Join(dir, item.Name())
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) && item.Type()&os.ModeSymlink != 0 {
				report.Dangling(path, item.Name(), err)
				entries = append(entries, Entry{Path: path, Rel: item.Name(), Target: LinkTarget(path)})
			} else {
				report.Problem("stat " + path + ": " + err.Error())
			}
			continue
		}
		if info.IsDir() {
			entries = append(entries, Entry{Path: path, Rel: item.Name(), SkillDir: true})
		}
	}
	return entries
}

func (p *Project) SkillsIn(dir string, report Reporter) []Entry {
	if p == nil {
		return SkillDirectories(dir, report)
	}
	key := "skills:" + dir
	cached, ok := p.directories[key]
	if !ok && p.frozen {
		return nil
	}
	if !ok {
		collect := Reporter{Problem: func(s string) { cached.Problems = append(cached.Problems, s) }, Warning: func(s string) { cached.Warnings = append(cached.Warnings, s) }, Dangling: func(path, rel string, err error) {
			cached.Unresolved = append(cached.Unresolved, Unresolved{Path: path, Rel: rel, Err: err})
		}}
		cached.Entries = SkillDirectories(dir, collect)
		p.directories[key] = cached
	}
	for _, s := range cached.Problems {
		report.Problem(s)
	}
	for _, s := range cached.Warnings {
		report.Warning(s)
	}
	for _, u := range cached.Unresolved {
		report.Dangling(u.Path, u.Rel, u.Err)
	}
	return cached.Entries
}

func (p *Project) LinkTarget(path string) string {
	if p == nil {
		return LinkTarget(path)
	}
	for _, dir := range p.directories {
		for _, u := range dir.Unresolved {
			if u.Path == path {
				return u.Target
			}
		}
		for _, e := range dir.Entries {
			if e.Path == path && e.Target != "" {
				return e.Target
			}
		}
	}
	for _, u := range p.Unresolved {
		if u.Path == path {
			return u.Target
		}
	}
	return "unreadable source link"
}
