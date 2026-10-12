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
	cfgDir := filepath.Join(home, ".config")
	if e := os.MkdirAll(filepath.Join(cfgDir, "engine-sync"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(root, ".claude"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(cfgDir, "engine-sync", "config.json"), []byte(`{"version":1,"codex":{"modelMap":{"fast":"machine-model"}},"machine":{"rewrites":[{"literal":"before","replace":"after"}]}}`), 0600); e != nil {
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
	machine := filepath.Join(home, ".config", "engine-sync", "config.json")
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
