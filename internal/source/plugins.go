package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// Plugin is one Claude plugin of machine.plugins whose root resolved, with
// the command and skill sources found under it. Its agents join the machine
// agent set as Agents carrying it.
type Plugin struct {
	Name, Root string
	// ConfigDir is the Claude config dir whose records named Root.
	ConfigDir string
	// Commands are {Root}/commands/**/*.md; each Rel is "{Name}/{rel}", so
	// the command id and twin name follow the machine command flattening.
	Commands []Entry
	Skills   []PluginSkill
}

// PluginSkill is one {Root}/skills/{dir}/SKILL.md. Rel is "{plugin}/{dir}";
// Files are the skill directory's other entries, by base name.
type PluginSkill struct {
	Path, Rel string
	Files     []string
}

// PluginFailure is a plugin whose records could not be read: "could not
// look", never "found nothing". Its installed twins are kept as they are.
type PluginFailure struct {
	Plugin, Problem string
	// Others are the "{other}-" prefixes of machine.plugins entries named
	// "{Plugin}-…", whose twins Kept never reserves.
	Others []string
}

// Kept reserves, in the twin directory dir, every entry named "{plugin}-…"
// but another plugin's: the failed plugin's twins stay exactly as they are.
func (f PluginFailure) Kept(dir string) artifact.Artifact {
	return artifact.Artifact{Path: filepath.Join(dir, f.Plugin+"-"), Kept: &artifact.SourceLink{Path: "plugin " + f.Plugin, Prefix: f.Plugin + "-", Except: f.Others}}
}

// PluginTwinName is the Codex and OpenCode name of the Claude plugin id
// "{plugin}:{name}": the colon becomes a dash, the flattening machine
// commands already take.
func PluginTwinName(plugin, name string) string { return plugin + "-" + name }

// PluginMarkerSource is what a plugin twin's marker credits: the plugin and
// the source path inside its root, never a host-absolute path.
func PluginMarkerSource(plugin *Plugin, path string) string {
	rel, err := filepath.Rel(plugin.Root, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	return "plugin:" + plugin.Name + "/" + filepath.ToSlash(rel)
}

// pluginRecords is what one Claude config dir records about its plugins.
// Each record file is read once; a file that exists but cannot be read or
// parsed is its error, never an empty record.
type pluginRecords struct {
	dir                         string
	settings, known, installed  map[string]json.RawMessage
	settingsErr, knownErr, iErr error
}

func readRecord(path string) (map[string]json.RawMessage, error) {
	raw, err := paths.ReadRegular(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err // ReadRegular's errors already name path
	}
	record := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return record, nil
}

func loadPluginRecords(dir string) pluginRecords {
	r := pluginRecords{dir: dir}
	r.settings, r.settingsErr = readRecord(filepath.Join(dir, "settings.json"))
	r.known, r.knownErr = readRecord(filepath.Join(dir, "plugins", "known_marketplaces.json"))
	r.installed, r.iErr = readRecord(filepath.Join(dir, "plugins", "installed_plugins.json"))
	return r
}

// enabled lists the sorted "{plugin}@{marketplace}" keys of
// settings.json's enabledPlugins whose plugin part is plugin and whose value
// is true.
func (r pluginRecords) enabled(plugin string) ([]string, error) {
	if r.settingsErr != nil {
		return nil, r.settingsErr
	}
	var enabled map[string]json.RawMessage
	if raw, ok := r.settings["enabledPlugins"]; ok {
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return nil, fmt.Errorf("parse %s: enabledPlugins: %w", filepath.Join(r.dir, "settings.json"), err)
		}
	}
	var keys []string
	for key, value := range enabled {
		name, _, found := strings.Cut(key, "@")
		var on bool
		if found && name == plugin && json.Unmarshal(value, &on) == nil && on {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// root resolves one enabled "{plugin}@{marketplace}" key: a directory
// marketplace's listed source, else the install record's installPath. An
// empty root with no error is a key no record resolves.
func (r pluginRecords) root(plugin, key string) (string, error) {
	_, marketplace, _ := strings.Cut(key, "@")
	if r.knownErr != nil {
		return "", r.knownErr
	}
	var known struct {
		Source struct {
			Source string `json:"source"`
		} `json:"source"`
		InstallLocation string `json:"installLocation"`
	}
	if raw, ok := r.known[marketplace]; ok && json.Unmarshal(raw, &known) == nil && known.Source.Source == "directory" && known.InstallLocation != "" {
		listing := filepath.Join(known.InstallLocation, ".claude-plugin", "marketplace.json")
		record, err := readRecord(listing)
		if err != nil {
			return "", err
		}
		var plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		}
		if raw, ok := record["plugins"]; ok {
			if err := json.Unmarshal(raw, &plugins); err != nil {
				return "", fmt.Errorf("parse %s: plugins: %w", listing, err)
			}
		}
		for _, entry := range plugins {
			var rel string
			if entry.Name == plugin && json.Unmarshal(entry.Source, &rel) == nil && rel != "" {
				return filepath.Join(known.InstallLocation, filepath.FromSlash(rel)), nil
			}
		}
	}
	if r.iErr != nil {
		return "", r.iErr
	}
	var installs map[string][]struct {
		InstallPath string `json:"installPath"`
	}
	if raw, ok := r.installed["plugins"]; ok {
		if err := json.Unmarshal(raw, &installs); err != nil {
			return "", fmt.Errorf("parse %s: plugins: %w", filepath.Join(r.dir, "plugins", "installed_plugins.json"), err)
		}
	}
	if records := installs[key]; len(records) > 0 {
		return records[0].InstallPath, nil
	}
	return "", nil
}

// ResolvePlugins is the one door to a Claude plugin's root: for each plugin
// name and each Claude config dir, in order, the enabledPlugins keys of
// settings.json, then per key the directory marketplace's listed source or
// the install record's installPath. The first config dir that resolves a
// plugin wins; a later dir naming another root is a warning. A plugin no
// config dir enables, or that no record resolves, is a warning naming each
// path it looked at; a record that exists but cannot be read or parsed, or a
// resolved root that is not a directory, is a failure.
func ResolvePlugins(names, configDirs []string) (resolved []Plugin, failed []PluginFailure, warnings []string) {
	records := make([]pluginRecords, 0, len(configDirs))
	for _, dir := range configDirs {
		records = append(records, loadPluginRecords(dir))
	}
	for _, name := range names {
		var found *Plugin
		// absent holds why a dir did not resolve the plugin: said only when
		// no dir resolves it, never beside twins another dir built.
		var problems, absent []string
		problem := func(s string) {
			if found != nil {
				// A dir after the winner can only ever cost a warning.
				warnings = append(warnings, fmt.Sprintf("%s; compiling %s from %s", s, found.Root, found.ConfigDir))
				return
			}
			problems = append(problems, s)
		}
		for _, r := range records {
			keys, err := r.enabled(name)
			if err != nil {
				problem(fmt.Sprintf("plugin %s: could not read whether it is enabled: %v", name, err))
				continue
			}
			if len(keys) == 0 {
				absent = append(absent, fmt.Sprintf("plugin %s: not enabled in %s (enabledPlugins has no %s@… set to true); no twins", name, filepath.Join(r.dir, "settings.json"), name))
				continue
			}
			for _, key := range keys {
				root, err := r.root(name, key)
				if err != nil {
					problem(fmt.Sprintf("plugin %s: could not resolve %s in %s: %v", name, key, r.dir, err))
					continue
				}
				if root == "" {
					absent = append(absent, fmt.Sprintf("plugin %s: %s is enabled in %s but neither %s nor %s resolves its root; no twins", name, key, filepath.Join(r.dir, "settings.json"), filepath.Join(r.dir, "plugins", "known_marketplaces.json"), filepath.Join(r.dir, "plugins", "installed_plugins.json")))
					continue
				}
				if info, err := os.Stat(root); err != nil || !info.IsDir() {
					if err == nil {
						err = errors.New("not a directory")
					}
					problem(fmt.Sprintf("plugin %s: root %s of %s in %s: %v", name, root, key, r.dir, err))
					continue
				}
				if found == nil {
					found = &Plugin{Name: name, Root: root, ConfigDir: r.dir}
				} else if filepath.Clean(found.Root) != filepath.Clean(root) {
					warnings = append(warnings, fmt.Sprintf("plugin %s: %s in %s resolves to %s; compiling %s from %s", name, key, r.dir, root, found.Root, found.ConfigDir))
				}
			}
		}
		if found == nil {
			warnings = append(warnings, absent...)
		}
		if len(problems) > 0 {
			failed = append(failed, PluginFailure{Plugin: name, Problem: strings.Join(problems, "; "), Others: longerPlugins(name, names)})
		}
		if found != nil {
			resolved = append(resolved, *found)
		}
	}
	return resolved, failed, warnings
}

// longerPlugins are the names of names that start with "{name}-": their
// twins share a failed name's "{name}-" prefix and are never its to keep.
func longerPlugins(name string, names []string) []string {
	var longer []string
	for _, other := range names {
		if strings.HasPrefix(other, name+"-") {
			longer = append(longer, other+"-")
		}
	}
	return longer
}

// loadPlugins resolves names against configDirs and snapshots every agent,
// command and skill source of each resolved plugin into p before it
// freezes. agents adds each plugin agent to p.Agents, named by its twin;
// a commands project leaves them out and keeps only the agent roster.
func (p *Project) loadPlugins(names, configDirs []string, agents bool) {
	if len(names) == 0 {
		return
	}
	resolved, failed, warnings := ResolvePlugins(names, configDirs)
	p.Warnings = append(p.Warnings, warnings...)
	p.PluginFailures = append(p.PluginFailures, failed...)
	for _, failure := range failed {
		// Its agent ids are unknown: the prefix compiles every
		// "{plugin}:{name}" to "{plugin}-{name}", the twin name a readable
		// record gives an agent. An id that is no agent (a skill's
		// "{plugin}:{dir}") flattens too, where a readable record leaves it.
		if p.AgentRoster == nil {
			p.AgentRoster = map[string]string{}
		}
		p.AgentRoster[failure.Plugin+":"] = PluginTwinName(failure.Plugin, "")
	}
	report := Reporter{Problem: func(s string) { p.Problems = append(p.Problems, s) }, Warning: func(s string) { p.Warnings = append(p.Warnings, s) }, Dangling: func(path, rel string, err error) {
		p.Warnings = append(p.Warnings, fmt.Sprintf("dangling source %s: %v", path, err))
	}}
	for i := range resolved {
		plugin := &resolved[i]
		dir := filepath.Join(plugin.Root, "agents")
		items, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			// "Could not look", never "no agents": every agent twin is kept.
			failure := PluginFailure{Plugin: plugin.Name, Problem: fmt.Sprintf("plugin %s: list %s: %v", plugin.Name, dir, err), Others: longerPlugins(plugin.Name, names)}
			p.PluginFailures = append(p.PluginFailures, failure)
			if p.AgentRoster == nil {
				p.AgentRoster = map[string]string{}
			}
			p.AgentRoster[plugin.Name+":"] = PluginTwinName(plugin.Name, "")
		}
		for _, item := range items {
			if !item.IsDir() && strings.HasSuffix(item.Name(), ".md") {
				p.loadPluginAgent(plugin, filepath.Join(dir, item.Name()), agents)
			}
		}
		for _, e := range p.Markdown(filepath.Join(plugin.Root, "commands"), nil, report) {
			if e.SkillDir {
				continue
			}
			_, _ = p.Read(e.Path)
			e.Rel = filepath.Join(plugin.Name, e.Rel)
			plugin.Commands = append(plugin.Commands, e)
		}
		for _, e := range p.SkillsIn(filepath.Join(plugin.Root, "skills"), report) {
			if !e.SkillDir {
				continue
			}
			file := filepath.Join(e.Path, "SKILL.md")
			if _, err := p.Read(file); errors.Is(err, fs.ErrNotExist) {
				continue // a directory with no SKILL.md is no skill
			}
			items, err := os.ReadDir(e.Path)
			if err != nil {
				p.Problems = append(p.Problems, fmt.Sprintf("plugin %s: read %s: %v", plugin.Name, e.Path, err))
				continue
			}
			skill := PluginSkill{Path: file, Rel: filepath.Join(plugin.Name, e.Rel)}
			for _, item := range items {
				if item.Name() != "SKILL.md" {
					skill.Files = append(skill.Files, item.Name())
				}
			}
			plugin.Skills = append(plugin.Skills, skill)
		}
	}
	p.Plugins = append(p.Plugins, resolved...)
}

// loadPluginAgent reads one {root}/agents/*.md: its Claude id
// "{plugin}:{name}" joins the agent roster, and with agents set it joins
// p.Agents named by its twin. A source that cannot be read or parsed is a
// problem naming it, its twins kept.
func (p *Project) loadPluginAgent(plugin *Plugin, path string, agents bool) {
	stem := strings.TrimSuffix(filepath.Base(path), ".md")
	if stem == "README" {
		return
	}
	if p.AgentRoster == nil {
		p.AgentRoster = map[string]string{}
	}
	raw, err := p.Read(path)
	if err == nil {
		var front Frontmatter
		var body string
		if front, body, err = Parse(string(raw)); err == nil {
			p.addPluginAgent(plugin, path, stem, raw, front, body, agents)
			return
		}
		p.Problems = append(p.Problems, DescribeParseError(path, err))
	} else {
		p.Problems = append(p.Problems, fmt.Sprintf("plugin %s: read %s: %v", plugin.Name, path, err))
	}
	// A broken agent keeps every twin it may be installed under, and every
	// reference to it still compiles to that twin name.
	for _, name := range failedSourceNames(path, raw) {
		twin := PluginTwinName(plugin.Name, name)
		p.AgentRoster[plugin.Name+":"+name] = twin
		p.Failed = append(p.Failed, twin)
	}
}

// addPluginAgent joins one parsed plugin agent to the roster and, with agents
// set, to p.Agents, unless a machine agent, loaded or failed, holds its twin
// name.
func (p *Project) addPluginAgent(plugin *Plugin, path, stem string, raw []byte, front Frontmatter, body string, agents bool) {
	name := strings.TrimSpace(front.Fields["name"])
	if name == "" {
		name = stem
	}
	twin := PluginTwinName(plugin.Name, name)
	p.AgentRoster[plugin.Name+":"+name] = twin
	if !agents {
		return
	}
	machine := ""
	if i := slices.IndexFunc(p.Agents, func(a Agent) bool { return a.Plugin == nil && a.Name == twin }); i >= 0 {
		machine = p.Agents[i].Path
	} else if slices.Contains(p.Failed, twin) {
		machine = twin + " (its source did not build)"
	}
	if machine != "" {
		p.Problems = append(p.Problems, fmt.Sprintf("plugin %s: agent %s from %s has the twin name of the machine agent %s; the machine agent keeps it, the plugin agent is skipped", plugin.Name, twin, path, machine))
		return
	}
	p.Agents = append(p.Agents, Agent{Name: twin, Path: path, Raw: raw, Front: front, Body: body, Plugin: plugin})
}
