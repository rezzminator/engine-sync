package cli

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedConfigEntryValidation(t *testing.T) {
	for _, tc := range []struct{ name, data, key string }{
		{"unknown", `{"version":1,"mystery":true}`, "mystery"},
		{"wrong type", `{"version":1,"codex":{"globalCommands":"yes"}}`, "codex.globalCommands"},
		{"nested unknown", `{"version":1,"opencode":{"mystery":true}}`, "opencode.mystery"},
		{"nested type", `{"version":1,"opencode":{"permission":{"edit":42}}}`, "opencode.permission.edit"},
		{"invalid action", `{"version":1,"opencode":{"permission":{"edit":{"src/**":"yes"}}}}`, "opencode.permission.edit"},
		{"null", `{"version":1,"opencode":null}`, "opencode"},
		{"unsafe path", `{"version":1,"workbenchMarker":"../escape"}`, "workbenchMarker"},
		{"version", `{"version":2}`, "version"},
		{"unreadable", "directory", "config"},
	} {
		for _, layer := range []string{"project", "machine"} {
			t.Run(layer+"/"+tc.name, func(t *testing.T) {
				root, home := testjail.Scratch(t), testjail.Scratch(t)
				putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
				target, name := root, ".claude/engine-sync.json"
				if layer == "machine" {
					target, name = home, ".claude/engine-sync.json"
				}
				path := filepath.Join(target, filepath.FromSlash(name))
				if tc.data == "directory" {
					if err := os.MkdirAll(path, 0755); err != nil {
						t.Fatal(err)
					}
				} else {
					putProjectFile(t, target, name, tc.data)
				}
				e := env{get: func(key string) string {
					switch key {
					case "HOME":
						return home
					case "CLAUDE_PROJECT_DIR":
						return root
					}
					return ""
				}}
				commands := [][]string{{"build", root}, {"opencode", "build", root}, {"doctor", root}}
				if layer == "machine" {
					commands = append(commands, []string{"global", "--source-repo", root, "--plan"}, []string{"doctor", "--machine"}, []string{"codex", "agents"})
				}
				for _, args := range commands {
					var out, errOut bytes.Buffer
					code := dispatch(args, &out, &errOut, e)
					text := out.String() + errOut.String()
					wantCode := 1
					if len(args) > 0 && args[0] == "hook" {
						wantCode = 0
					}
					if code != wantCode || !strings.Contains(text, path) || !strings.Contains(text, tc.key) {
						t.Errorf("%v rc=%d output=%s; want path and key %q", args, code, text, tc.key)
					}
				}
			})
		}
	}
}

// A configuration the loader refuses by rule — a removed key, a moved file, a
// key the file may not hold, a value outside its pattern — exits 2 at every
// door and names the key and the file; other config errors keep exit 1.
func TestConfigRefusalsExitTwo(t *testing.T) {
	const old = ".config/engine-sync/config.json"
	for _, tc := range []struct {
		name, layer, file, data string
		args                    func(root string) []string
		want                    []string
	}{
		{"bad managedBy", "machine", ".claude/engine-sync.json", `{"version":1,"machine":{"managedBy":"Pro fessor"}}`,
			func(string) []string { return []string{"config", "show"} }, []string{"machine.managedBy", ".claude/engine-sync.json"}},
		{"old config path", "machine", old, `{"version":1}`,
			func(string) []string { return []string{"config", "show"} }, []string{old, "is no longer read", ".claude/engine-sync.json"}},
		{"old targets path", "machine", ".config/engine-sync/targets.json", `{"codexHomes":[]}`,
			func(string) []string { return []string{"global", "--plan"} }, []string{".config/engine-sync/targets.json", `under "targets"`, ".claude/engine-sync.json"}},
		{"targets in the project file", "project", ".claude/engine-sync.json", `{"version":1,"targets":{}}`,
			func(root string) []string { return []string{"config", "show", root} }, []string{"targets: unknown key", ".claude/engine-sync.json"}},
		{"removed store key", "machine", ".claude/engine-sync.json", `{"version":1,"machine":{"generatedAgentsDir":".local/state/x"}}`,
			func(string) []string { return []string{"doctor", "--machine"} }, []string{"machine.generatedAgentsDir is removed", ".claude/engine-sync.json"}},
		{"removed legacy key", "machine", ".claude/engine-sync.json", `{"version":1,"machine":{"legacyCodexAgentsDir":".local/state/x"}}`,
			func(root string) []string { return []string{"build", root} }, []string{"machine.legacyCodexAgentsDir is removed", ".claude/engine-sync.json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, home := testjail.Scratch(t), testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
			target := home
			if tc.layer == "project" {
				target = root
			}
			putProjectFile(t, target, tc.file, tc.data)
			e := env{get: func(key string) string {
				if key == "HOME" {
					return home
				}
				return ""
			}}
			rc, out := auditRun(e, tc.args(root)...)
			if rc != 2 {
				t.Fatalf("%v rc=%d, want 2: %s", tc.args(root), rc, out)
			}
			for _, text := range tc.want {
				if !strings.Contains(out, text) {
					t.Errorf("output lacks %q: %s", text, out)
				}
			}
		})
	}
	t.Run("hook start prints the refusal", func(t *testing.T) {
		home := testjail.Scratch(t)
		putProjectFile(t, home, old, `{"version":1}`)
		e := env{get: func(key string) string {
			if key == "HOME" {
				return home
			}
			return ""
		}}
		rc, out := auditRun(e, "hook", "claude", "start")
		if rc != 0 || !strings.Contains(out, old) || !strings.Contains(out, "is no longer read") {
			t.Fatalf("hook start rc=%d, want 0 printing the refusal: %s", rc, out)
		}
	})
}
