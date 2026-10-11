package config

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
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
	if len(cfg.Codex.ModelMap)+len(cfg.OpenCode.ModelMap)+len(cfg.Machine.ModelMap) != 0 {
		t.Fatalf("neutral models contain pins: %#v", cfg)
	}
}

func TestReviewLayeredPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, machine, project, want string
		bad                          bool
	}{
		{"bare permission", `{"version":1,"opencode":{"permission":"deny","agent":{"reader":{"permission":"deny"}}}}`, `{"version":1,"opencode":{"permission":{"bash":"allow"},"agent":{"reader":{"permission":{"bash":"allow"}}}}}`, "", false},
		{"duplicate", `{"version":1,"version":1}`, "", "duplicate", true},
		{"nested duplicate", `{"version":1,"codex":{"suffixMode":"none","suffixMode":"project"}}`, "", "duplicate", true},
		{"relative env", "", "", "CLAUDE_CONFIG_DIR", true},
		{"legacy null", "", `{"version":1,"rootAdapter":null}`, "", false},
		{"legacy adapter", "", `{"version":1,"rootAdapter":"A ${name} B"}`, "", false},
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
			put(filepath.Join(home, ".config", "engine-sync", "config.json"), tc.machine)
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
			if tc.name == "bare permission" {
				for _, p := range []Permission{cfg.OpenCode.Permission, cfg.OpenCode.Agent["reader"].Permission} {
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
