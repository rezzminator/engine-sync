package config

import (
	"errors"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineRewriteAndTemplateLayers(t *testing.T) {
	home := testjail.Scratch(t)
	root := testjail.Scratch(t)
	if e := os.MkdirAll(filepath.Join(home, ".claude"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(root, ".claude"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(home, ".claude", "engine-sync.json"), []byte(`{"version":1,"codex":{"modelMap":{"fast":"machine-model"}},"machine":{"rewrites":[{"literal":"before","replace":"after"}]}}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, ".claude", "engine-sync.json"), []byte(`{"version":1,"codex":{"agentPreamble":"${name} {{.Engine}} {{.Source}} {{.Root}}"},"rewrites":[{"literal":"x","replace":"y","engines":["codex"],"kinds":["agent"]}]}`), 0600); e != nil {
		t.Fatal(e)
	}
	cfg, e := Resolve(root, home, func(string) string { return "" })
	if e != nil {
		t.Fatal(e)
	}
	text, e := RenderTemplate(cfg.Codex.AgentPreamble, TemplateData{Name: "reader", Engine: "Codex", Source: "reader.md", Root: "AGENTS.md"})
	if e != nil || text != "reader Codex reader.md AGENTS.md" || len(cfg.Deprecations) != 1 || len(cfg.Machine.Rewrites) != 1 || cfg.Codex.ModelMap["fast"] != "machine-model" {
		t.Fatalf("cfg=%#v text=%q err=%v", cfg, text, e)
	}
	if !strings.Contains(cfg.Sources["codex.agentPreamble"], "engine-sync.json") {
		t.Fatal("template source missing")
	}
}

func TestProjectLayerCompletesMachineLayer(t *testing.T) {
	home := testjail.Scratch(t)
	root := testjail.Scratch(t)
	machine := filepath.Join(home, ".claude", "engine-sync.json")
	project := filepath.Join(root, ".claude", "engine-sync.json")
	for path, body := range map[string]string{
		machine: `{"version":1,"codex":{"suffixMode":"strip-prefix"}}`,
		project: `{"version":1,"codex":{"suffixPrefix":"team-"}}`,
	} {
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	cfg, e := Resolve(root, home, func(string) string { return "" })
	if e != nil || cfg.Codex.SuffixMode != "strip-prefix" || cfg.Codex.SuffixPrefix != "team-" {
		t.Fatalf("layered strip-prefix = %q %q, err %v", cfg.Codex.SuffixMode, cfg.Codex.SuffixPrefix, e)
	}
	if _, e := Resolve("", home, func(string) string { return "" }); e == nil || !strings.Contains(e.Error(), machine+": codex.suffixPrefix required") {
		t.Fatalf("machine scope alone = %v, want the machine file named", e)
	}
}

func TestLegacyOpenCodeFileIsDeprecated(t *testing.T) {
	home := testjail.Scratch(t)
	root := testjail.Scratch(t)
	legacy := filepath.Join(root, ".claude", "opencode-build.json")
	if e := os.MkdirAll(filepath.Dir(legacy), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(legacy, []byte(`{"modelMap":{"opus":"legacy-model"}}`), 0600); e != nil {
		t.Fatal(e)
	}
	cfg, e := Resolve(root, home, func(string) string { return "" })
	want := legacy + " is deprecated; use " + filepath.Join(root, RelPath) + " opencode.modelMap"
	if e != nil || cfg.OpenCode.ModelMap["opus"] != "legacy-model" || len(cfg.Deprecations) != 1 || cfg.Deprecations[0] != want {
		t.Fatalf("legacy OpenCode map = %q, deprecations %q, err %v; want %q", cfg.OpenCode.ModelMap, cfg.Deprecations, e, want)
	}
}

func TestErrorSourceIsTheKeysOwnLayer(t *testing.T) {
	cfg := Config{Source: "project.json", Sources: map[string]string{
		"codex.modelMap.o":    "machine.json",
		"codex.modelMap.opus": "flag",
		"codex.modelMap":      "default",
	}}
	for range 50 {
		if got := sourceForError(cfg, errors.New("codex.modelMap.opus: bad model")); got != "flag" {
			t.Fatalf("source = %q, want the key's own layer", got)
		}
	}
}

func TestLegacyTextIsLiteralNotATemplate(t *testing.T) {
	home := testjail.Scratch(t)
	root := testjail.Scratch(t)
	legacy := filepath.Join(root, ".claude", "codex-build.json")
	if e := os.MkdirAll(filepath.Dir(legacy), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(legacy, []byte(`{"version":1,"rootAdapter":"Write {{ braces }} and {{.Root}} for ${name}."}`), 0600); e != nil {
		t.Fatal(e)
	}
	cfg, e := Resolve(root, home, func(string) string { return "" })
	if e != nil {
		t.Fatal(e)
	}
	if got, e := RenderTemplate(cfg.Codex.RootAdapter, TemplateData{Name: "reader", Root: "AGENTS.md"}); e != nil || got != "Write {{ braces }} and {{.Root}} for ${name}." {
		t.Fatalf("legacy adapter = %q (%v)", got, e)
	}
}

func putConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}

// The one global policy file is {home}/.claude/engine-sync.json whatever the
// environment says; it carries `machine` and `targets`.
func TestGlobalFileIsHomeClaudeEngineSyncJSON(t *testing.T) {
	home := testjail.Scratch(t)
	want := filepath.Join(home, ".claude", "engine-sync.json")
	if got := MachinePath(home); got != want {
		t.Fatalf("MachinePath = %s, want %s", got, want)
	}
	codexHome := filepath.Join(home, ".codex")
	putConfigFile(t, want, `{"version":1,"machine":{"managedBy":"app"},"targets":{"codexHomes":["`+codexHome+`"]}}`)
	cfg, e := Resolve("", home, func(key string) string {
		if key == "XDG_CONFIG_HOME" || key == "CLAUDE_CONFIG_DIR" {
			return filepath.Join(home, "elsewhere")
		}
		return ""
	})
	if e != nil {
		t.Fatalf("Resolve: %v", e)
	}
	if cfg.Machine.ManagedBy != "app" || cfg.Sources["machine.managedBy"] != want {
		t.Fatalf("managedBy=%q source=%q, want app from %s", cfg.Machine.ManagedBy, cfg.Sources["machine.managedBy"], want)
	}
	if cfg.Targets == nil || cfg.Targets.CodexHomes == nil || len(*cfg.Targets.CodexHomes) != 1 || (*cfg.Targets.CodexHomes)[0] != codexHome || cfg.Sources["targets.codexHomes"] != want {
		t.Fatalf("targets=%+v source=%q, want the Codex home from %s", cfg.Targets, cfg.Sources["targets.codexHomes"], want)
	}
	listed := false
	for _, row := range Describe(cfg) {
		if row.Key == "targets.codexHomes" && strings.Contains(string(row.Value), codexHome) && row.Source == want {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("Describe lists no targets.codexHomes row from %s: %+v", want, Describe(cfg))
	}
}

// A machine without targets prints no targets row: config show is unchanged.
func TestDescribeOmitsUnsetTargets(t *testing.T) {
	for _, row := range Describe(Default()) {
		if strings.HasPrefix(row.Key, "targets") || row.Key == "machine.managedBy" {
			t.Fatalf("default config lists %s", row.Key)
		}
	}
}

func TestOldGlobalPathsAreRefusedLoudly(t *testing.T) {
	for _, tc := range []struct {
		name, old, want string
		xdg             bool
	}{
		{"config.json", "config.json", `%s is no longer read; move its keys into %s`, false},
		{"targets.json", "targets.json", `%s is no longer read; move its keys under "targets" in %s`, false},
		{"XDG config.json", "config.json", `%s is no longer read; move its keys into %s`, true},
		{"XDG targets.json", "targets.json", `%s is no longer read; move its keys under "targets" in %s`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := testjail.Scratch(t)
			configHome := filepath.Join(home, ".config")
			env := func(string) string { return "" }
			if tc.xdg {
				configHome = testjail.Scratch(t)
				env = func(key string) string {
					if key == "XDG_CONFIG_HOME" {
						return configHome
					}
					return ""
				}
			}
			old := filepath.Join(configHome, "engine-sync", tc.old)
			putConfigFile(t, old, `{"version":1}`)
			global := filepath.Join(home, ".claude", "engine-sync.json")
			want := "engine-sync: " + strings.Replace(tc.want, "%s", old, 1)
			want = strings.Replace(want, "%s", global, 1)
			for _, root := range []string{"", testjail.Scratch(t)} {
				_, e := Resolve(root, home, env)
				if e == nil || e.Error() != want || !IsRefusal(e) {
					t.Fatalf("Resolve(%q) = %v (refusal %v), want %q", root, e, IsRefusal(e), want)
				}
				if _, _, e := LoadTargets(env, home); e == nil || e.Error() != want {
					t.Fatalf("LoadTargets = %v, want %q", e, want)
				}
			}
			putConfigFile(t, global, `{"version":1}`)
			cfg, e := Resolve("", home, env)
			if e != nil || len(cfg.Deprecations) != 0 {
				t.Fatalf("with both files: %v %v, want the new file read and nothing said of the old", e, cfg.Deprecations)
			}
		})
	}
	// A regular file where an old directory would be holds no old policy:
	// the defaults load, as with no old path at all.
	t.Run("config dir is a file", func(t *testing.T) {
		home := testjail.Scratch(t)
		putConfigFile(t, filepath.Join(home, ".config"), "not a directory\n")
		if _, e := Resolve("", home, func(string) string { return "" }); e != nil {
			t.Fatalf("Resolve = %v, want the defaults", e)
		}
		if _, found, e := LoadTargets(func(string) string { return "" }, home); e != nil || found {
			t.Fatalf("LoadTargets = %v %v, want no targets and no error", found, e)
		}
	})
}

func TestProjectFileRefusesGlobalOnlyKeys(t *testing.T) {
	for _, key := range []string{"targets", "machine"} {
		t.Run(key, func(t *testing.T) {
			home, root := testjail.Scratch(t), testjail.Scratch(t)
			path := filepath.Join(root, RelPath)
			putConfigFile(t, path, `{"version":1,"`+key+`":{}}`)
			_, e := Resolve(root, home, func(string) string { return "" })
			if e == nil || !IsRefusal(e) || !strings.Contains(e.Error(), path) || !strings.Contains(e.Error(), key+": unknown key") {
				t.Fatalf("Resolve = %v (refusal %v), want a refusal naming %s and %s", e, IsRefusal(e), key, path)
			}
		})
	}
}
