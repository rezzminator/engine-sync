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
