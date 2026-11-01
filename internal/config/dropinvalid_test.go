package config

import (
	"slices"
	"testing"
)

func TestDropInvalidKeepsValidEntriesAndWarnsPerDroppedOne(t *testing.T) {
	cfg := Default()
	cfg.Sources = map[string]string{"codex.neverRegister": "f.json", "machine.retiredAgents": "f.json", "agents.passthroughKeys": "f.json", "machine.managedBy": "f.json"}
	cfg.Codex.NeverRegister = []string{"reader", "a/b", ".."}
	cfg.Machine.RetiredAgents = []string{"old agent", "gone"}
	cfg.Agents.PassthroughKeys = []string{"color", "bad:key"}
	cfg.Machine.ManagedBy = "Bad Name"
	dropInvalid(&cfg)
	if !slices.Equal(cfg.Codex.NeverRegister, []string{"reader"}) || !slices.Equal(cfg.Machine.RetiredAgents, []string{"gone"}) || !slices.Equal(cfg.Agents.PassthroughKeys, []string{"color"}) || cfg.Machine.ManagedBy != "" {
		t.Fatalf("kept: %v %v %v %q", cfg.Codex.NeverRegister, cfg.Machine.RetiredAgents, cfg.Agents.PassthroughKeys, cfg.Machine.ManagedBy)
	}
	want := []string{
		`config f.json: codex.neverRegister: invalid entry "a/b", ignored`,
		`config f.json: codex.neverRegister: invalid entry "..", ignored`,
		`config f.json: machine.retiredAgents: invalid entry "old agent", ignored`,
		`config f.json: agents.passthroughKeys: invalid entry "bad:key", ignored`,
		`config f.json: machine.managedBy: "Bad Name" must match ^[a-z][a-z0-9-]{0,31}$; ignored, no managed-by line is written`,
	}
	if !slices.Equal(cfg.Warnings, want) {
		t.Fatalf("warnings\nwant %q\n got %q", want, cfg.Warnings)
	}
}
