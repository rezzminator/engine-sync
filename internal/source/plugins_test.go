package source

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/testjail"
)

// pluginFixture writes files under root; "{root}" in a body stands for root.
func pluginFixture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(body, "{root}", root)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// claudeDir is one Claude config dir enabling flights@team-market from the
// directory marketplace at {root}/market, whose listing puts flights at
// plugins/flights; installed_plugins.json names a cache copy.
func claudeDir(dir string) map[string]string {
	return map[string]string{
		dir + "/settings.json":                     `{"enabledPlugins":{"flights@team-market":true,"other@team-market":true}}`,
		dir + "/plugins/known_marketplaces.json":   `{"team-market":{"source":{"source":"directory","path":"{root}/market"},"installLocation":"{root}/market"}}`,
		dir + "/plugins/installed_plugins.json":    `{"version":2,"plugins":{"flights@team-market":[{"scope":"user","installPath":"{root}/cache/flights"}]}}`,
		"market/.claude-plugin/marketplace.json":   `{"name":"team-market","plugins":[{"name":"flights","source":"./plugins/flights"}]}`,
		"market/plugins/flights/agents/lander.md":  "---\nname: lander\ndescription: Land.\n---\nLand.\n",
		"cache/flights/agents/lander.md":           "---\nname: lander\ndescription: Land.\n---\nLand.\n",
		"elsewhere/flights/agents/lander.md":       "---\nname: lander\ndescription: Land.\n---\nLand.\n",
		"market/plugins/flights/.claude-plugin/ok": "",
	}
}

func TestResolvePluginsFindsEachRootOrNamesWhyNot(t *testing.T) {
	for _, tc := range []struct {
		name string
		// edit changes the fixture: a rel path to a new body, "" deletes it.
		edit     map[string]string
		dirs     []string
		root     string
		failures []string
		warnings []string
		// from is the dir expected to resolve it; empty is the first.
		from string
	}{
		{name: "directory marketplace listing", root: "{root}/market/plugins/flights"},
		{name: "other marketplace falls back to installPath",
			edit: map[string]string{"a/plugins/known_marketplaces.json": `{"team-market":{"source":{"source":"github","repo":"x/y"},"installLocation":"{root}/market"}}`},
			root: "{root}/cache/flights"},
		{name: "directory marketplace not listing the plugin falls back to installPath",
			edit: map[string]string{"market/.claude-plugin/marketplace.json": `{"name":"team-market","plugins":[{"name":"other","source":"./plugins/other"}]}`},
			root: "{root}/cache/flights"},
		{name: "not enabled",
			edit:     map[string]string{"a/settings.json": `{"enabledPlugins":{"flights@team-market":false}}`},
			warnings: []string{"plugin flights: not enabled in {root}/a/settings.json (enabledPlugins has no flights@… set to true); no twins"}},
		{name: "no settings file is not enabled",
			edit:     map[string]string{"a/settings.json": ""},
			warnings: []string{"plugin flights: not enabled in {root}/a/settings.json (enabledPlugins has no flights@… set to true); no twins"}},
		{name: "enabled but no record resolves it",
			edit:     map[string]string{"a/plugins/known_marketplaces.json": "", "a/plugins/installed_plugins.json": ""},
			warnings: []string{"plugin flights: flights@team-market is enabled in {root}/a/settings.json but neither {root}/a/plugins/known_marketplaces.json nor {root}/a/plugins/installed_plugins.json resolves its root; no twins"}},
		{name: "unreadable settings",
			edit:     map[string]string{"a/settings.json": `{"enabledPlugins":`},
			failures: []string{"plugin flights: could not read whether it is enabled: parse {root}/a/settings.json: unexpected end of JSON input"}},
		{name: "unreadable marketplace record",
			edit:     map[string]string{"a/plugins/known_marketplaces.json": `{"team-market":`},
			failures: []string{"plugin flights: could not resolve flights@team-market in {root}/a: parse {root}/a/plugins/known_marketplaces.json: unexpected end of JSON input"}},
		{name: "unreadable install record",
			edit:     map[string]string{"a/plugins/known_marketplaces.json": "", "a/plugins/installed_plugins.json": `[`},
			failures: []string{"plugin flights: could not resolve flights@team-market in {root}/a: parse {root}/a/plugins/installed_plugins.json: unexpected end of JSON input"}},
		{name: "root is not a directory",
			edit:     map[string]string{"market/.claude-plugin/marketplace.json": `{"plugins":[{"name":"flights","source":"./plugins/flights/agents/lander.md"}]}`},
			failures: []string{"plugin flights: root {root}/market/plugins/flights/agents/lander.md of flights@team-market in {root}/a: not a directory"}},
		{name: "first config dir wins",
			edit: map[string]string{
				"b/settings.json":                   `{"enabledPlugins":{"flights@local":true}}`,
				"b/plugins/installed_plugins.json":  `{"plugins":{"flights@local":[{"installPath":"{root}/elsewhere/flights"}]}}`,
				"b/plugins/known_marketplaces.json": `{}`,
			},
			dirs:     []string{"a", "b"},
			root:     "{root}/market/plugins/flights",
			warnings: []string{"plugin flights: flights@local in {root}/b resolves to {root}/elsewhere/flights; compiling {root}/market/plugins/flights from {root}/a"}},
		{name: "a later dir that cannot be read only warns",
			edit:     map[string]string{"b/settings.json": `{"enabledPlugins":`},
			dirs:     []string{"a", "b"},
			root:     "{root}/market/plugins/flights",
			warnings: []string{"plugin flights: could not read whether it is enabled: parse {root}/b/settings.json: unexpected end of JSON input; compiling {root}/market/plugins/flights from {root}/a"}},
		{name: "an earlier dir that cannot be read fails",
			edit:     map[string]string{"b/settings.json": `{"enabledPlugins":`},
			dirs:     []string{"b", "a"},
			from:     "a",
			root:     "{root}/market/plugins/flights",
			failures: []string{"plugin flights: could not read whether it is enabled: parse {root}/b/settings.json: unexpected end of JSON input"}},
		{name: "a dir not enabling it is silent when another resolves it",
			edit: map[string]string{"b/settings.json": `{"enabledPlugins":{}}`},
			dirs: []string{"b", "a"},
			from: "a",
			root: "{root}/market/plugins/flights"},
		{name: "every problem of a plugin is one failure",
			edit: map[string]string{"b/settings.json": `{"enabledPlugins":`, "a/settings.json": `{"enabledPlugins":`},
			dirs: []string{"a", "b"},
			failures: []string{"plugin flights: could not read whether it is enabled: parse {root}/a/settings.json: unexpected end of JSON input; " +
				"plugin flights: could not read whether it is enabled: parse {root}/b/settings.json: unexpected end of JSON input"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testjail.Scratch(t)
			pluginFixture(t, root, claudeDir("a"))
			for rel, body := range tc.edit {
				if body == "" {
					if err := os.Remove(filepath.Join(root, rel)); err != nil {
						t.Fatal(err)
					}
					continue
				}
				pluginFixture(t, root, map[string]string{rel: body})
			}
			dirs := []string{filepath.Join(root, "a")}
			if tc.dirs != nil {
				dirs = nil
				for _, d := range tc.dirs {
					dirs = append(dirs, filepath.Join(root, d))
				}
			}
			resolved, failed, warnings := ResolvePlugins([]string{"flights"}, dirs)
			at := func(s string) string { return strings.ReplaceAll(s, "{root}", root) }
			var gotRoot string
			if len(resolved) == 1 {
				gotRoot = resolved[0].Root
				from := dirs[0]
				if tc.from != "" {
					from = filepath.Join(root, tc.from)
				}
				if resolved[0].Name != "flights" || resolved[0].ConfigDir != from {
					t.Fatalf("resolved %+v, want flights from %s", resolved[0], from)
				}
			} else if len(resolved) > 1 {
				t.Fatalf("resolved %+v, want at most one", resolved)
			}
			if want := at(tc.root); gotRoot != want {
				t.Fatalf("root %q, want %q (failures %+v, warnings %q)", gotRoot, want, failed, warnings)
			}
			var problems []string
			for _, f := range failed {
				if f.Plugin != "flights" {
					t.Fatalf("failure %+v names the wrong plugin", f)
				}
				problems = append(problems, f.Problem)
			}
			var wantFailures, wantWarnings []string
			for _, s := range tc.failures {
				wantFailures = append(wantFailures, at(s))
			}
			for _, s := range tc.warnings {
				wantWarnings = append(wantWarnings, at(s))
			}
			if !reflect.DeepEqual(problems, wantFailures) {
				t.Fatalf("failures\n got %q\nwant %q", problems, wantFailures)
			}
			if !reflect.DeepEqual(warnings, wantWarnings) {
				t.Fatalf("warnings\n got %q\nwant %q", warnings, wantWarnings)
			}
		})
	}
}

// TestLoadMachinePluginsJoinTheAgentSet: a resolved plugin's agents join
// the machine set under their twin names, its commands and skills are
// collected, the roster maps every Claude id, and a plugin agent whose twin
// name a machine agent holds is skipped as a named problem.
func TestLoadMachinePluginsJoinTheAgentSet(t *testing.T) {
	root := testjail.Scratch(t)
	files := claudeDir("a")
	for rel, body := range map[string]string{
		"market/plugins/flights/agents/foreman.md":                   "---\nname: foreman\ndescription: Build.\n---\nBuild.\n",
		"market/plugins/flights/agents/README.md":                    "not an agent\n",
		"market/plugins/flights/commands/doctor.md":                  "---\ndescription: Meter.\n---\nMeter.\n",
		"market/plugins/flights/skills/inventory/SKILL.md":           "---\nname: inventory\ndescription: Count.\n---\nCount.\n",
		"market/plugins/flights/skills/inventory/inventory.py":       "print(1)\n",
		"market/plugins/flights/skills/empty/notes.txt":              "no skill\n",
		"agents/flights-foreman.md":                                  "---\nname: flights-foreman\ndescription: Machine.\n---\nMachine.\n",
		"agents/reader.md":                                           "---\nname: reader\ndescription: Read.\n---\nRead.\n",
		"market/plugins/flights/commands/nested/deep.md":             "---\ndescription: Deep.\n---\nDeep.\n",
		"market/plugins/flights/agents/notes/ignored-subdir-file.md": "---\nname: x\n---\n",
		"market/plugins/flights/agents/broken.md":                    "---\nname: wrecked\nengines:\n  codex: [unclosed\n---\nBroken.\n",
		"market/plugins/flights/agents/scout.md":                     "---\ndescription: Scout.\n---\nScout.\n",
		"agents/flights-scout.md":                                    "---\nname: flights-scout\nengines:\n  codex: [unclosed\n---\nMachine.\n",
	} {
		files[rel] = body
	}
	pluginFixture(t, root, files)
	p, err := LoadMachine(root, filepath.Join(root, "agents"), filepath.Join(root, "generated"), MachineOptions{ClaudeConfigDirs: []string{filepath.Join(root, "a")}, Plugins: []string{"flights"}})
	if err != nil {
		t.Fatal(err)
	}
	type agent struct{ Name, Path, Plugin string }
	var agents []agent
	for _, a := range p.Agents {
		plugin := ""
		if a.Plugin != nil {
			plugin = a.Plugin.Name
		}
		agents = append(agents, agent{a.Name, strings.TrimPrefix(a.Path, root+"/"), plugin})
	}
	wantAgents := []agent{{"flights-foreman", "agents/flights-foreman.md", ""}, {"reader", "agents/reader.md", ""}, {"flights-lander", "market/plugins/flights/agents/lander.md", "flights"}}
	if !reflect.DeepEqual(agents, wantAgents) {
		t.Fatalf("agents\n got %+v\nwant %+v", agents, wantAgents)
	}
	if want := map[string]string{"flights:foreman": "flights-foreman", "flights:lander": "flights-lander", "flights:broken": "flights-broken", "flights:wrecked": "flights-wrecked", "flights:scout": "flights-scout"}; !reflect.DeepEqual(p.AgentRoster, want) {
		t.Fatalf("roster %q, want %q", p.AgentRoster, want)
	}
	wantProblems := []string{"FRONTMATTER-INVALID " + root + "/agents/flights-scout.md:3 — did not find expected ',' or ']'",
		"FRONTMATTER-INVALID " + root + "/market/plugins/flights/agents/broken.md:3 — did not find expected ',' or ']'",
		"plugin flights: agent flights-foreman from " + root + "/market/plugins/flights/agents/foreman.md has the twin name of the machine agent " + root + "/agents/flights-foreman.md; the machine agent keeps it, the plugin agent is skipped",
		"plugin flights: agent flights-scout from " + root + "/market/plugins/flights/agents/scout.md has the twin name of the machine agent flights-scout (its source did not build); the machine agent keeps it, the plugin agent is skipped"}
	if !reflect.DeepEqual(p.Problems, wantProblems) {
		t.Fatalf("problems\n got %q\nwant %q", p.Problems, wantProblems)
	}
	if want := []string{"flights-scout", "flights-broken", "flights-wrecked"}; !reflect.DeepEqual(p.Failed, want) {
		t.Fatalf("failed %q, want %q", p.Failed, want)
	}
	if len(p.Plugins) != 1 {
		t.Fatalf("plugins %+v, want flights", p.Plugins)
	}
	var commands []string
	for _, e := range p.Plugins[0].Commands {
		commands = append(commands, e.Rel)
	}
	if want := []string{"flights/doctor.md", "flights/nested/deep.md"}; !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands %q, want %q", commands, want)
	}
	wantSkills := []PluginSkill{{Path: root + "/market/plugins/flights/skills/inventory/SKILL.md", Rel: "flights/inventory", Files: []string{"inventory.py"}}}
	if !reflect.DeepEqual(p.Plugins[0].Skills, wantSkills) {
		t.Fatalf("skills\n got %+v\nwant %+v", p.Plugins[0].Skills, wantSkills)
	}
}

// TestPluginFailureNeverKeepsALongerPluginsTwins: the "{plugin}-" reservation
// of a failed plugin excepts each machine.plugins name it prefixes.
func TestPluginFailureNeverKeepsALongerPluginsTwins(t *testing.T) {
	root := testjail.Scratch(t)
	pluginFixture(t, root, map[string]string{"a/settings.json": `{"enabledPlugins":`})
	_, failed, _ := ResolvePlugins([]string{"tool", "tool-visibility", "toolbox"}, []string{filepath.Join(root, "a")})
	var got [][]string
	for _, f := range failed {
		got = append(got, f.Kept("/codex/agents").Kept.Except)
	}
	if want := [][]string{{"tool-visibility-"}, nil, nil}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kept exceptions %q, want %q", got, want)
	}
}
