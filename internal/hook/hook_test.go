package hook

import (
	"bytes"
	"encoding/json"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testjail.RunTests(m)) }
func TestDirtyFlagsDistinguishSameNamedProjects(t *testing.T) {
	home := testjail.Scratch(t)
	e := Context{Home: func() (string, error) { return home, nil }, State: func(string) string { return filepath.Join(home, "state") }}
	a, err := DirtyFlag(e, filepath.Join(home, "one", "repo"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := DirtyFlag(e, filepath.Join(home, "two", "repo"))
	if err != nil || a == b || filepath.Dir(a) != filepath.Dir(b) {
		t.Fatalf("%s %s %v", a, b, err)
	}
}

func TestReviewStopRetainsFlagDuringStages(t *testing.T) {
	home := testjail.Scratch(t)
	root := filepath.Join(home, "project")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "engine-sync.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	e := Context{Home: func() (string, error) { return home, nil }, Get: func(key string) string {
		if key == "CLAUDE_PROJECT_DIR" {
			return root
		}
		return ""
	}, State: func(string) string { return filepath.Join(home, "state") }, Policy: func(string, string, io.Writer) (*config.Config, error) { c := config.Default(); return &c, nil }, Writer: func() string { return "/fixture/bin/engine-sync" }, Engines: []string{"sample"}}
	payload := Payload{}
	payload.ToolInput.FilePath = filepath.Join(root, "CLAUDE.md")
	var out, stderr bytes.Buffer
	Mark(payload, &stderr, e)
	flag, err := DirtyFlag(e, root)
	if err != nil {
		t.Fatal(err)
	}
	marked := false
	Stop(Payload{}, &out, &stderr, e, func([]string) (int, string) {
		if _, err := os.Lstat(flag); err != nil {
			t.Errorf("kill during this stage loses dirty flag: %v", err)
		}
		if !marked {
			Mark(payload, &stderr, e)
			marked = true
		}
		return 0, ""
	})
	if _, err := os.Lstat(flag); err != nil {
		t.Fatalf("concurrent mark was erased: %v output=%s error=%s", err, &out, &stderr)
	}
}

func TestHookPluginLogFailures(t *testing.T) {
	for _, name := range []string{"mark-lock", "stop-stage", "start-stage"} {
		t.Run(name, func(t *testing.T) {
			home := testjail.Scratch(t)
			root := filepath.Join(home, "project")
			logs := filepath.Join(home, "logs")
			if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".claude", "engine-sync.json"), []byte(`{"version":1}`), 0600); err != nil {
				t.Fatal(err)
			}
			e := Context{Home: func() (string, error) { return home, nil }, Get: func(k string) string {
				switch k {
				case "CLAUDE_PROJECT_DIR":
					return root
				case "PLUGIN_LOG_DIR":
					return logs
				case "HOME":
					return home
				}
				return ""
			}, State: func(string) string { return filepath.Join(home, "state") }, Policy: func(string, string, io.Writer) (*config.Config, error) { c := config.Default(); return &c, nil }, Targets: func(string) (bool, error) { return true, nil }, Writer: func() string { return "fixture" }, Engines: []string{"sample"}}
			var out, stderr bytes.Buffer
			p := Payload{}
			p.ToolInput.FilePath = filepath.Join(root, "CLAUDE.md")
			event, where := "hook-step-failed", "internal/hook/hook.go#Stop"
			switch name {
			case "mark-lock":
				event, where = "hook-lock-failed", "internal/hook/hook.go#Mark"
				if err := os.WriteFile(e.State(home), []byte("block"), 0600); err != nil {
					t.Fatal(err)
				}
				if code := Mark(p, &stderr, e); code != 0 {
					t.Fatal(code)
				}
			case "stop-stage":
				Mark(p, &stderr, e)
				if code := Stop(p, &out, &stderr, e, func([]string) (int, string) { return 1, "fixture stage failure" }); code != 0 {
					t.Fatal(code)
				}
			case "start-stage":
				where = "internal/hook/hook.go#Start"
				if code := Start(&stderr, e, func(stage []string) (int, string) {
					if stage[0] == "global" {
						return 1, "fixture global failure"
					}
					return 0, ""
				}); code != 0 {
					t.Fatal(code)
				}
			}
			raw, err := os.ReadFile(filepath.Join(logs, "engine-sync", "errors.jsonl"))
			if err != nil {
				t.Fatalf("hook failure row missing: %v", err)
			}
			found := false
			for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
				var row map[string]any
				if err := json.Unmarshal(line, &row); err != nil {
					t.Fatal(err)
				}
				if row["event"] == event && row["where"] == where && row["level"] == "error" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s %s: %s", event, where, raw)
			}
		})
	}
}
