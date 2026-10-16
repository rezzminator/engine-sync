package config

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetsExplicitEmptyBeatsEnvironment(t *testing.T) {
	home := testjail.Scratch(t)
	cfg := Default()
	empty := []string{}
	result, err := ResolveTargets(func(key string) string {
		if key == "CODEX_HOME" {
			return filepath.Join(home, "env-codex")
		}
		return ""
	}, home, cfg, Targets{CodexHomes: &empty})
	if err != nil || len(*result.CodexHomes) != 0 || result.ClaudeConfigDirs[0] != filepath.Join(home, ".claude") {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestTargetsDistinguishesAbsentEmptyAndMalformed(t *testing.T) {
	home := testjail.Scratch(t)
	get := func(string) string { return "" }
	if _, found, err := LoadTargets(get, home); found || err != nil {
		t.Fatal(found, err)
	}
	path := MachinePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"version":1,"targets":{"codexHomes":[]}}`, `{"version":1,"targets":{"codexHomes":["relative"]}}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		got, found, err := LoadTargets(get, home)
		if strings.Contains(raw, `"codexHomes":[]`) {
			if err != nil || !found || got.CodexHomes == nil || len(*got.CodexHomes) != 0 {
				t.Fatalf("%+v %v", got, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), path) {
			t.Fatal(err)
		}
	}
}

// a targets object without codexHomes means the same Codex homes as no targets
// at all; a capabilities-only object is valid.
func TestTargetsWithoutCodexHomesKeepsTheDefaultHomes(t *testing.T) {
	home := testjail.Scratch(t)
	get := func(string) string { return "" }
	path := MachinePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"targets":{"capabilities":["gpt-agents"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, found, err := LoadTargets(get, home)
	if err != nil || !found || got.CodexHomes != nil || len(got.Capabilities) != 1 {
		t.Fatalf("LoadTargets = %+v, %v, %v; want the capabilities-only file", got, found, err)
	}
	result, err := ResolveTargets(get, home, Default(), Targets{})
	if err != nil || len(*result.CodexHomes) != 1 || (*result.CodexHomes)[0] != filepath.Join(home, ".codex") {
		t.Fatalf("ResolveTargets = %+v, %v; want the default Codex home", result, err)
	}
}

// A global file without a `targets` object is no targets: found stays false.
func TestTargetsAbsentFromAGlobalFile(t *testing.T) {
	home := testjail.Scratch(t)
	putConfigFile(t, MachinePath(home), `{"version":1}`)
	if got, found, err := LoadTargets(func(string) string { return "" }, home); err != nil || found || got.CodexHomes != nil {
		t.Fatalf("LoadTargets = %+v, %v, %v; want none", got, found, err)
	}
}
