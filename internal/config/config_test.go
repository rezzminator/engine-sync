package config

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTemplateConfigValidationAndNeutralModels(t *testing.T) {
	home := testjail.Scratch(t)
	root := testjail.Scratch(t)
	dir := filepath.Join(root, ".claude")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "engine-sync.json")
	if e := os.WriteFile(path, []byte(`{"version":1,"codex":{"agentPreamble":"{{.Unknown}}"}}`), 0600); e != nil {
		t.Fatal(e)
	}
	_, e := Resolve(root, home, func(string) string { return "" })
	if e == nil || !strings.Contains(e.Error(), path) || !strings.Contains(e.Error(), "codex.agentPreamble") {
		t.Fatalf("bad template error=%v", e)
	}
	cfg := Default()
	if len(cfg.Codex.ModelMap)+len(cfg.OpenCode.ModelMap) != 0 {
		t.Fatalf("neutral models contain pins: %#v", cfg)
	}
}

func TestReviewLayeredPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, machine, project, want string
		bad                          bool
		// warns are the exact warnings of a good run; {machine} and
		// {project} stand for the two files' paths.
		warns []string
	}{
		{"bare permission", `{"version":1,"opencode":{"permission":"deny"}}`, `{"version":1,"opencode":{"permission":{"bash":"allow"}}}`, "", false, nil},
		{"machine declarations replace carrier", `{"version":1,"machine":{"variants":"custom.json"}}`, "", "machine.variants is removed; use {machine.sourceAgentsDir}/{name}.md declarations", true, nil},
		{"machine empty carrier refused", `{"version":1,"machine":{"variants":""}}`, "", "machine.variants is removed; use {machine.sourceAgentsDir}/{name}.md declarations", true, nil},
		{"machine modelMap removed", `{"version":1,"machine":{"modelMap":{"opus":"model-a"}}}`, "", "machine.modelMap is removed; use codex.modelMap", true, nil},
		{"opencode agent removed", "", `{"version":1,"opencode":{"agent":{"reader":{"permission":"deny"}}}}`, "opencode.agent is removed; use engines.opencode.permission", true, nil},
		{"missing model alias", `{"version":1,"codex":{"modelMap":{"opus":"model-a"}}}`, `{"version":1,"codex":{"rootAdapter":"Use {{index .Models \"opus\"}} or {{index .Models \"haiku\"}}."}}`, "Use model-a or haiku.", false, []string{`config {project}: codex.rootAdapter: model alias "haiku" is not in the engine's modelMap; it renders as "haiku"`}},
		{"unknown key", `{"version":1,"codex":{"suffixMode":"none","mystery":true},"later":{}}`, `{"version":1,"opencode":{"mystery":1}}`, "", false, []string{"config {machine}: codex.mystery: unknown key, ignored", "config {machine}: later: unknown key, ignored", "config {project}: opencode.mystery: unknown key, ignored"}},
		{"invalid entries", `{"version":1,"codex":{"neverRegister":["kept","a b"]},"machine":{"retiredAgents":["..","old"]},"agents":{"passthroughKeys":["x-key","a:b"]},"targets":{"capabilities":["gpt"," "]}}`, "", "", false, []string{`config {machine}: codex.neverRegister: invalid entry "a b", ignored`, `config {machine}: machine.retiredAgents: invalid entry "..", ignored`, `config {machine}: agents.passthroughKeys: invalid entry "a:b", ignored`, `config {machine}: targets.capabilities: invalid entry " ", ignored`}},
		{"absent version", `{"codex":{"suffixMode":"none"}}`, `{"opencode":{}}`, "", false, nil},
		{"duplicate", `{"version":1,"version":1}`, "", "duplicate", true, nil},
		{"nested duplicate", `{"version":1,"codex":{"suffixMode":"none","suffixMode":"project"}}`, "", "duplicate", true, nil},
		{"relative env", "", "", "CLAUDE_CONFIG_DIR", true, nil},
		{"legacy null", "", `{"version":1,"rootAdapter":null}`, "", false, nil},
		{"legacy adapter", "", `{"version":1,"rootAdapter":"A ${name} B"}`, "", false, nil},
		{"legacy opencode agent removed", "", `{"version":1,"opencode":{"agent":{"reader":{"permission":"deny"}}}}`, "opencode.agent is removed; use engines.opencode.permission", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := testjail.Scratch(t), testjail.Scratch(t)
			put := func(path, body string) {
				t.Helper()
				if body == "" {
					return
				}
				if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(path, []byte(body), 0600); e != nil {
					t.Fatal(e)
				}
			}
			put(filepath.Join(home, ".claude", "engine-sync.json"), tc.machine)
			rel := RelPath
			if strings.HasPrefix(tc.name, "legacy") {
				rel = LegacyRelPath
			}
			put(filepath.Join(root, rel), tc.project)
			cfg, e := Resolve(root, home, func(k string) string {
				if tc.name == "relative env" && k == "CLAUDE_CONFIG_DIR" {
					return "relative"
				}
				return ""
			})
			if tc.bad {
				if e == nil || !strings.Contains(e.Error(), tc.want) {
					t.Fatalf("want %s error, got %v", tc.want, e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			var warns []string
			for _, w := range tc.warns {
				warns = append(warns, strings.NewReplacer("{machine}", MachinePath(home), "{project}", filepath.Join(root, rel)).Replace(w))
			}
			if !reflect.DeepEqual(cfg.Warnings, warns) {
				t.Fatalf("warnings %q, want %q", cfg.Warnings, warns)
			}
			switch tc.name {
			case "missing model alias":
				if got, e := RenderTemplate(cfg.Codex.RootAdapter, TemplateData{Models: cfg.Codex.ModelMap}); e != nil || got != tc.want {
					t.Fatalf("adapter %q error %v, want %q", got, e, tc.want)
				}
			case "unknown key", "absent version":
				if cfg.Version != 1 || cfg.Codex.SuffixMode != "none" {
					t.Fatalf("the rest of the file was lost: version %d suffixMode %q", cfg.Version, cfg.Codex.SuffixMode)
				}
			case "invalid entries":
				if !reflect.DeepEqual(cfg.Codex.NeverRegister, []string{"kept"}) || !reflect.DeepEqual(cfg.Machine.RetiredAgents, []string{"old"}) || !reflect.DeepEqual(cfg.Agents.PassthroughKeys, []string{"x-key"}) || !reflect.DeepEqual(cfg.Targets.Capabilities, []string{"gpt"}) {
					t.Fatalf("kept entries: %q %q %q %+v", cfg.Codex.NeverRegister, cfg.Machine.RetiredAgents, cfg.Agents.PassthroughKeys, cfg.Targets)
				}
			}
			if tc.name == "bare permission" {
				for _, p := range []Permission{cfg.OpenCode.Permission} {
					if len(p.Tools) != 2 || p.Tools[0].Name != "*" || p.Tools[0].Value.Action != "deny" {
						t.Fatalf("lost deny: %+v", p)
					}
				}
			}
			if tc.name == "legacy adapter" {
				got, e := RenderTemplate(cfg.Codex.RootAdapter, TemplateData{Engine: "Codex"})
				if e != nil || got != "A ${name} B" {
					t.Fatalf("adapter %q error %v", got, e)
				}
			}
		})
	}
	cfg := Default()
	_, e := cfg.WithCodexOverrides(CodexOverrides{SuffixMode: "bogus"})
	if e == nil || !strings.Contains(e.Error(), "flag --suffix-mode") {
		t.Fatalf("flag blame: %v", e)
	}
}
