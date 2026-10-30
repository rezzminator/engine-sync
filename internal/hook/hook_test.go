package hook

import (
	"bytes"
	"encoding/json"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/guard"
	"github.com/rezzminator/engine-sync/internal/pluginlog"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// A dirty lock another live process holds after green stages is no fault: the
// flag stays for the next turn, an info row records it, nothing reaches the screen.
func TestStopHeldDirtyLockStaysOffScreen(t *testing.T) {
	home := testjail.Scratch(t)
	root := filepath.Join(home, "project")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "engine-sync.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, "state")
	e := Context{Home: func() (string, error) { return home, nil }, Get: func(key string) string {
		if key == "CLAUDE_PROJECT_DIR" {
			return root
		}
		return ""
	}, State: func(string) string { return state }, Policy: func(string, string, io.Writer) (*config.Config, error) { c := config.Default(); return &c, nil }, Writer: func() string { return "/fixture/bin/engine-sync" }, Engines: []string{"sample"}}
	payload := Payload{}
	payload.ToolInput.FilePath = filepath.Join(root, "CLAUDE.md")
	var out, stderr bytes.Buffer
	Mark(payload, &stderr, e)
	flag, err := DirtyFlag(e, root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := guard.Lock(state, "dirty:"+root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	Stop(Payload{}, &out, &stderr, e, func([]string) (int, string) { return 0, "" })
	if out.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("held dirty lock reached the screen: stdout=%q stderr=%q", &out, &stderr)
	}
	if _, err := os.Lstat(flag); err != nil {
		t.Errorf("held dirty lock lost the flag: %v", err)
	}
	info, err := os.ReadFile(filepath.Join(pluginlog.Dir("", home, "engine-sync"), "info.jsonl"))
	if err != nil || !strings.Contains(string(info), `"event":"lock-busy"`) {
		t.Errorf("no lock-busy info row: %v %s", err, info)
	}
}

// A SessionStart build or global another live process holds the lock for is no
// fault: an info row each, nothing on screen, and the flag hands the check to Stop.
func TestStartBusyStagesStayOffScreen(t *testing.T) {
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
	}, State: func(string) string { return filepath.Join(home, "state") }, Targets: func(string) (bool, error) { return true, nil }}
	var stderr bytes.Buffer
	Start(&stderr, e, func(stage []string) (int, string) {
		if stage[0] == "check" {
			return 1, "stale"
		}
		return guard.BusyCode, "BUSY lock /state/locks/x.lock: holder pid=1; deadline exceeded\n"
	})
	if stderr.Len() != 0 {
		t.Errorf("busy start stages reached the screen: %q", &stderr)
	}
	flag, err := DirtyFlag(e, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(flag); err != nil {
		t.Errorf("busy start build left no dirty flag: %v", err)
	}
	dir := pluginlog.Dir("", home, "engine-sync")
	if raw, err := os.ReadFile(filepath.Join(dir, "errors.jsonl")); err == nil {
		t.Errorf("busy start stages wrote error rows: %s", raw)
	}
	info, err := os.ReadFile(filepath.Join(dir, "info.jsonl"))
	if err != nil || strings.Count(string(info), `"event":"lock-busy"`) != 2 {
		t.Errorf("want two lock-busy info rows: %v %s", err, info)
	}
}

// A build another live process holds the lock for skips that engine's check
// (the holder is writing the mirror) and stays off screen; a real failure still blocks.
func TestStopBusyBuildStaysOffScreen(t *testing.T) {
	for _, form := range []string{"busy", "busy and failed"} {
		t.Run(form, func(t *testing.T) {
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
			}, State: func(string) string { return filepath.Join(home, "state") }, Policy: func(string, string, io.Writer) (*config.Config, error) { c := config.Default(); return &c, nil }, Writer: func() string { return "/fixture/bin/engine-sync" }, Engines: []string{"codex", "opencode"}}
			payload := Payload{}
			payload.ToolInput.FilePath = filepath.Join(root, "CLAUDE.md")
			var out, stderr bytes.Buffer
			Mark(payload, &stderr, e)
			flag, err := DirtyFlag(e, root)
			if err != nil {
				t.Fatal(err)
			}
			var ran []string
			Stop(Payload{}, &out, &stderr, e, func(stage []string) (int, string) {
				ran = append(ran, stage[0]+" "+stage[1])
				if stage[0] == "codex" && stage[1] == "build" {
					return guard.BusyCode, "BUSY lock /state/locks/x.lock: holder pid=1 root=/r; deadline exceeded\n"
				}
				if form == "busy and failed" && stage[0] == "opencode" && stage[1] == "check" {
					return 1, "stale mirror\n"
				}
				return 0, ""
			})
			if strings.Join(ran, ",") != "codex build,opencode build,opencode check" {
				t.Errorf("stages run = %v, want codex check skipped", ran)
			}
			if _, err := os.Lstat(flag); err != nil {
				t.Errorf("busy build lost the flag: %v", err)
			}
			if form == "busy" {
				if out.Len() != 0 || stderr.Len() != 0 {
					t.Errorf("busy build reached the screen: stdout=%q stderr=%q", &out, &stderr)
				}
				info, err := os.ReadFile(filepath.Join(pluginlog.Dir("", home, "engine-sync"), "info.jsonl"))
				if err != nil || !strings.Contains(string(info), `"event":"lock-busy"`) {
					t.Errorf("no lock-busy info row: %v %s", err, info)
				}
				return
			}
			if !strings.Contains(out.String(), `"decision":"block"`) || !strings.Contains(out.String(), "opencode check") {
				t.Errorf("a real failure beside a busy build must still block: %s", &out)
			}
		})
	}
}
