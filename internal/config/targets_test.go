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
	path := filepath.Join(home, ".config", "engine-sync", "targets.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"codexHomes":[]}`, `{"codexHomes":["relative"]}`, `{}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		got, found, err := LoadTargets(get, home)
		if raw == `{"codexHomes":[]}` {
			if err != nil || !found || got.CodexHomes == nil || len(*got.CodexHomes) != 0 {
				t.Fatalf("%+v %v", got, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), path) {
			t.Fatal(err)
		}
	}
}
