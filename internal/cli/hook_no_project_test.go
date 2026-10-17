package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/testjail"
)

// Drive the exported process entry so all three hooks must keep both streams
// silent, avoid failure logs, and leave project and machine state untouched.
func TestHooksWithoutProjectAreSilent(t *testing.T) {
	for _, rootCase := range []string{"explicit-home", "explicit-ancestor", "undiscovered", "discovered-home"} {
		for _, step := range []string{"stop", "start", "mark"} {
			t.Run(rootCase+"/"+step, func(t *testing.T) {
				home := testjail.Scratch(t)
				t.Setenv("HOME", home)
				t.Setenv("ENGINE_SYNC_STATE", filepath.Join(home, "state"))
				t.Setenv("PLUGIN_LOG_DIR", filepath.Join(home, "logs"))
				t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))
				cwd := home
				explicit := ""
				switch rootCase {
				case "explicit-home":
					explicit = home
				case "explicit-ancestor":
					explicit = filepath.Dir(home)
				case "undiscovered", "discovered-home":
					cwd = filepath.Join(home, "unconfigured")
				}
				if rootCase == "discovered-home" {
					putProjectFile(t, home, "CLAUDE.md", "# Home notes\n")
				}
				putProjectFile(t, cwd, ".claude/agents/reader.md", "---\nname: reader\ndescription: Reads.\ntools: Read\n---\nRead only.\n")
				t.Setenv("CLAUDE_PROJECT_DIR", explicit)
				t.Chdir(cwd)
				body := `{"session_id":"no-project","tool_input":{"file_path":` + strconvQuote(filepath.Join(cwd, ".claude", "agents", "reader.md")) + `}}`
				var out, stderr bytes.Buffer
				if code := Main([]string{"hook", "claude", step}, &out, &stderr, strings.NewReader(body), Version); code != 0 {
					t.Errorf("hook exited %d, want 0", code)
				}
				if out.Len() != 0 || stderr.Len() != 0 {
					t.Errorf("no-project hook must be silent: stdout=%q stderr=%q", &out, &stderr)
				}
				for _, level := range []string{"errors", "warnings"} {
					path := filepath.Join(home, "logs", "engine-sync", level+".jsonl")
					if raw, err := os.ReadFile(path); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("no-project hook must not write %s: %s (err=%v)", level, raw, err)
					}
				}
				// start still runs its machine refresh, which needs no project.
				if step == "start" {
					return
				}
				for _, rel := range []string{"state", ".codex", ".config/opencode", "data", "xdg-state"} {
					if _, err := os.Lstat(filepath.Join(home, filepath.FromSlash(rel))); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("no-project hook changed %s: %v", rel, err)
					}
				}
			})
		}
	}
}

// A filesystem error and a bad global policy joined with a no-project error
// are real failures, not evidence that a hook can silently skip the turn.
func TestHookRootFailuresStillReport(t *testing.T) {
	for _, rootCase := range []string{"physical-path", "policy-and-no-project"} {
		for _, step := range []string{"stop", "start", "mark"} {
			t.Run(rootCase+"/"+step, func(t *testing.T) {
				home := testjail.Scratch(t)
				t.Setenv("HOME", home)
				t.Setenv("ENGINE_SYNC_STATE", filepath.Join(home, "state"))
				t.Setenv("PLUGIN_LOG_DIR", filepath.Join(home, "logs"))
				t.Chdir(home)
				explicit := ""
				want := "resolve physical path"
				if rootCase == "physical-path" {
					putProjectFile(t, home, "not-a-directory", "file\n")
					explicit = filepath.Join(home, "not-a-directory", "child")
				} else {
					putProjectFile(t, home, ".claude/engine-sync.json", "{bad")
					want = "engine-sync.json"
				}
				t.Setenv("CLAUDE_PROJECT_DIR", explicit)
				body := `{"tool_input":{"file_path":` + strconvQuote(filepath.Join(home, ".claude", "agents", "reader.md")) + `}}`
				var out, stderr bytes.Buffer
				if code := Main([]string{"hook", "claude", step}, &out, &stderr, strings.NewReader(body), Version); code != 0 {
					t.Errorf("hook exited %d, want 0", code)
				}
				if !strings.Contains(stderr.String(), "engine-sync hook claude "+step+":") || !strings.Contains(stderr.String(), want) {
					t.Errorf("root failure lost its diagnostic: %q", &stderr)
				}
				if step == "stop" {
					var reply struct {
						Message string `json:"systemMessage"`
					}
					if err := json.Unmarshal(out.Bytes(), &reply); err != nil || !strings.Contains(reply.Message, want) {
						t.Errorf("root failure lost its Stop notice: %q (%v)", &out, err)
					}
				} else if out.Len() != 0 {
					t.Errorf("%s unexpectedly wrote stdout: %q", step, &out)
				}
				raw, err := os.ReadFile(filepath.Join(home, "logs", "engine-sync", "errors.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
					var row struct {
						Level, Event, Where, Msg string
					}
					if err := json.Unmarshal(line, &row); err != nil {
						t.Fatal(err)
					}
					if row.Level == "error" && row.Event == "hook-step-failed" && strings.HasSuffix(strings.ToLower(row.Where), "#"+step) && strings.Contains(row.Msg, want) {
						found = true
					}
				}
				if !found {
					t.Errorf("root failure lost its error row: %s", raw)
				}
			})
		}
	}
}
