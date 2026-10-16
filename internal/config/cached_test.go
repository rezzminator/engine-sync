package config

import (
	"bytes"
	"encoding/json"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCachedFailureAndNotice(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	p := filepath.Join(root, filepath.FromSlash(RelPath))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{bad`), 0600); err != nil {
		t.Fatal(err)
	}
	cache := map[string]CachedResult{}
	var out bytes.Buffer
	get := func(string) string { return "" }
	_, first := ResolveCached(root, home, get, cache, nil, &out)
	if first == nil {
		t.Fatal("invalid policy accepted")
	}
	if err := os.WriteFile(p, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, again := ResolveCached(root, home, get, cache, nil, &out)
	if again == nil || again.Error() != first.Error() {
		t.Fatalf("failed lookup lost: %v", again)
	}
	if _, err := ResolveCached(root, home, get, nil, nil, &out); err != nil {
		t.Fatal(err)
	}
}

func TestCachedPluginLogDeprecation(t *testing.T) {
	root, home, logs := testjail.Scratch(t), testjail.Scratch(t), testjail.Scratch(t)
	p := filepath.Join(root, filepath.FromSlash(LegacyRelPath))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	get := func(k string) string {
		if k == "PLUGIN_LOG_DIR" {
			return logs
		}
		if k == "HOME" {
			return home
		}
		return ""
	}
	cache, notices := map[string]CachedResult{}, map[string]bool{}
	var stderr bytes.Buffer
	for i := 0; i < 2; i++ {
		if _, err := ResolveCached(root, home, get, cache, notices, &stderr); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(logs, "engine-sync", "warnings.jsonl"))
	if err != nil {
		t.Fatalf("deprecation row missing: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("cached duplicate: %s", raw)
	}
	var row map[string]any
	if err := json.Unmarshal(lines[0], &row); err != nil {
		t.Fatal(err)
	}
	if row["event"] != "config-deprecated" || row["where"] != "internal/config/cached.go#ResolveCached" || row["level"] != "warning" || !strings.Contains(row["msg"].(string), "deprecated") {
		t.Fatalf("wrong deprecation: %s", raw)
	}
}
