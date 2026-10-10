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
					target, name = home, ".config/engine-sync/config.json"
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
