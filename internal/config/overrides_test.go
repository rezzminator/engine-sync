package config

import (
	"testing"
)

func TestFlagListsReplaceAndModelsMergeWithoutMutatingDefaults(t *testing.T) {
	cfg := Default()
	cfg.Codex.ModelMap = map[string]string{"one": "first"}
	cfg.Codex.ExcludeDirs = []string{"old"}
	got, err := cfg.WithCodexOverrides(CodexOverrides{ModelMap: map[string]string{"two": "second"}, ExcludeDirs: []string{"new"}})
	if err != nil || got.Codex.ModelMap["one"] != "first" || got.Codex.ModelMap["two"] != "second" || len(got.Codex.ExcludeDirs) != 1 || got.Codex.ExcludeDirs[0] != "new" || len(cfg.Codex.ModelMap) != 1 {
		t.Fatalf("%+v %v", got.Codex, err)
	}
}
