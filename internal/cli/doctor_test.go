package cli

import (
	"bytes"
	"encoding/json"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchitectureLayersNativePolicyAndDoctorSources(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
	putProjectFile(t, root, ".claude/agents/reader.md", "---\nname: reader\ndescription: Reads.\nmodel: opus\n---\nRead.\n")
	configHome := filepath.Join(home, ".config")
	putProjectFile(t, configHome, "engine-sync/config.json", `{"version":1,"codex":{"modelMap":{"opus":"machine-model"},"rootAdapter":"Machine adapter.\n"},"opencode":{"permission":{"bash":{"*":"allow","deploy *":"deny","deploy preview":"allow"}},"agent":{"reader":{"permission":{"edit":{"private/**":"deny"}}}}},"machine":{"retiredAgents":["retired-reader"]}}`)
	putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1,"codex":{"modelMap":{"opus":"project-model"}},"opencode":{"agent":{"reader":{"permission":{"bash":{"deploy preview":"ask"}}}}},"mcp":{"globalServers":["central"]}}`)
	vars := map[string]string{"HOME": home, "XDG_CONFIG_HOME": configHome, "ENGINE_SYNC_WORKBENCH_MARKER": ".project/workbench.json"}
	configReads := 0
	e := env{get: func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			configReads++
		}
		return vars[key]
	}}
	var out, errOut bytes.Buffer
	if code := dispatch([]string{"build", root, "--model", "opus=flag-model"}, &out, &errOut, e); code != 0 {
		t.Fatalf("build rc=%d: %s%s", code, &out, &errOut)
	}
	if configReads != 1 {
		t.Fatalf("configuration resolutions=%d want 1", configReads)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".codex/agents/reader.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `model = "flag-model"`) {
		t.Fatalf("flags must win: %s", raw)
	}
	raw, err = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Machine adapter.") {
		t.Fatalf("machine adapter inherited: %s", raw)
	}
	raw, err = os.ReadFile(filepath.Join(root, ".opencode/opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(raw), `"deploy *"`) >= strings.Index(string(raw), `"deploy preview"`) {
		t.Fatalf("native rule order changed: %s", raw)
	}
	raw, err = os.ReadFile(filepath.Join(root, ".opencode/agent/reader.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"private/**": deny`) {
		t.Fatalf("machine per-agent rule inherited: %s", raw)
	}
	if !strings.Contains(string(raw), `"deploy preview": ask`) {
		t.Fatalf("native per-agent override: %s", raw)
	}
	out.Reset()
	errOut.Reset()
	if code := dispatch([]string{"doctor", root, "--model", "opus=flag-model"}, &out, &errOut, e); code != 0 {
		t.Fatalf("doctor rc=%d: %s%s", code, &out, &errOut)
	}
	for _, want := range []string{"codex.modelMap.opus", "flag --model", "codex.rootAdapter", filepath.Join(configHome, "engine-sync/config.json"), "ENGINE_SYNC_WORKBENCH_MARKER", "mcp.globalServers", filepath.Join(root, ".claude/engine-sync.json")} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor source lacks %q: %s", want, &out)
		}
	}
}

func TestMachineDoctorJSONNamesPolicySourcesAndConfigFailures(t *testing.T) {
	home := testjail.Scratch(t)
	path := filepath.Join(home, ".config", "engine-sync", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"version":1,"workbenchMarker":".workspace/marker.json"}`, `{"version":1,"unknown":true}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		var out, errout bytes.Buffer
		code := dispatch([]string{"doctor", "--machine", "--home", home, "--claude-absent", "--json"}, &out, &errout, env{get: func(k string) string {
			if k == "HOME" {
				return home
			}
			return ""
		}})
		var result struct {
			OK      bool                `json:"ok"`
			Rows    []map[string]string `json:"rows"`
			Sources []config.SourceRow  `json:"configSources"`
			Error   string              `json:"error"`
		}
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("decode %q: %v", out.String(), err)
		}
		if strings.Contains(raw, "unknown") {
			if code != 1 || result.OK || !strings.Contains(result.Error, path) || !strings.Contains(result.Error, "unknown") {
				t.Fatalf("%d %+v", code, result)
			}
		} else {
			found := false
			for _, row := range result.Sources {
				if row.Key == "workbenchMarker" && strings.Contains(row.Source, path) {
					found = true
				}
			}
			if code != 0 || !result.OK || !found {
				t.Fatalf("%d %+v", code, result)
			}
		}
	}
}
