package cli

import (
	"bytes"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func cliWriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	putProjectFile(t, filepath.Dir(path), filepath.Base(path), content)
}

func plantCLIUnreadableSource(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		if err := os.Symlink("/proc/self/mem", path); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(path, []byte("---\ndescription: Broken.\n---\nWork.\n"), 0o000); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCodeDanglingCommandWarnsOnceInBuildAndIsAProblemInCheck(t *testing.T) {
	for _, action := range []string{"build", "check", "doctor"} {
		t.Run(action, func(t *testing.T) {
			root := testjail.Scratch(t)
			home := filepath.Join(root, "home")
			cliWriteTestFile(
				t,
				filepath.Join(root, ".claude", "commands", "review.md"),
				"---\ndescription: Review.\n---\nReview.\n",
			)
			cliWriteTestFile(
				t,
				filepath.Join(root, ".claude", "agents", "worker.md"),
				"---\ndescription: Worker.\n---\nWork.\n",
			)
			dangling := filepath.Join(root, ".claude", "commands", "gone.md")
			if err := os.Symlink(filepath.Join(root, "nowhere.md"), dangling); err != nil {
				t.Fatal(err)
			}

			var stdout, stderr bytes.Buffer
			code := invokeOpenCodeCLI(
				[]string{action, root, "--home", home},
				func() (string, error) { return root, nil },
				home,
				&stdout,
				&stderr,
			)
			mentions := 0
			for _, line := range strings.Split(stderr.String(), "\n") {
				if strings.Contains(line, dangling) {
					mentions++
				}
			}
			if action == "build" {
				if code != 0 || mentions != 1 || !strings.Contains(stderr.String(), "warning: DANGLING "+dangling) {
					t.Fatalf(
						"build: code=%d mentions=%d stdout=%q stderr=%q",
						code,
						mentions,
						stdout.String(),
						stderr.String(),
					)
				}
				for _, path := range []string{
					filepath.Join(root, ".opencode", "command", "review.md"),
					filepath.Join(root, ".opencode", "agent", "worker.md"),
					filepath.Join(root, ".opencode", "opencode.jsonc"),
				} {
					if _, err := os.Stat(path); err != nil {
						t.Fatalf("build skipped healthy output %s: %v", path, err)
					}
				}
				return
			}
			if code != 1 || mentions != 1 || !strings.Contains(stderr.String(), "engine-sync opencode: DANGLING "+dangling) {
				t.Fatalf("%s: code=%d mentions=%d stderr=%q", action, code, mentions, stderr.String())
			}
		})
	}
}

// A broken source fails build and check naming it. A malformed MCP tool is no
// broken source: the role builds with the tool denied, and the warning names it.
func TestOpenCodeBrokenSourceFailsBuildAndCheck(t *testing.T) {
	const agent, agentOut = ".claude/agents/broken.md", ".opencode/agent/broken.md"
	writes := func(content string) func(*testing.T, string) {
		return func(t *testing.T, path string) { cliWriteTestFile(t, path, content) }
	}
	for _, door := range []struct {
		name    string
		source  string
		output  string
		plant   func(t *testing.T, path string)
		warning string
	}{
		{"unreadable agent", agent, agentOut, plantCLIUnreadableSource, ""},
		{"malformed agent frontmatter", agent, agentOut, writes("---\ndescription: Broken role.\nWork.\n"), ""},
		{
			"malformed agent tools", agent, agentOut, writes("---\ndescription: B.\ntools: mcp__broken\n---\nWork.\n"),
			`malformed MCP tool mcp__broken in %s — OpenCode MCP tool "mcp__broken" must be mcp__server__tool; it stays denied`,
		},
		{
			"malformed command frontmatter",
			".claude/commands/broken.md",
			".opencode/command/broken.md",
			writes("---\ndescription: Broken.\nReview.\n"),
			"",
		},
	} {
		for _, action := range []string{"build", "check"} {
			t.Run(door.name+"/"+action, func(t *testing.T) {
				root := testjail.Scratch(t)
				home := filepath.Join(root, "home")
				review := filepath.Join(root, ".claude", "commands", "review.md")
				cliWriteTestFile(t, review, "---\ndescription: Review.\n---\nReview.\n")
				worker := filepath.Join(root, ".claude", "agents", "worker.md")
				cliWriteTestFile(t, worker, "---\ndescription: Worker.\n---\nWork.\n")
				broken := filepath.Join(root, filepath.FromSlash(door.source))
				door.plant(t, broken)

				var stdout, stderr bytes.Buffer
				code := invokeOpenCodeCLI(
					[]string{action, root, "--home", home},
					func() (string, error) { return root, nil },
					home,
					&stdout,
					&stderr,
				)
				named := ""
				for _, line := range strings.Split(stderr.String(), "\n") {
					if strings.Contains(line, broken) {
						named = line
					}
				}
				if door.warning != "" {
					// check runs on a tree never built, so its outputs are MISSING.
					want, wantCode := "engine-sync opencode: warning: "+fmt.Sprintf(door.warning, broken), map[string]int{"build": 0, "check": 1}[action]
					if code != wantCode || named != want {
						t.Fatalf("%s: code=%d, want %d; named %q, want %q; stderr=%q", action, code, wantCode, named, want, stderr.String())
					}
					if _, err := os.Stat(filepath.Join(root, door.output)); action == "build" && err != nil {
						t.Fatalf("build did not write %s: %v", door.output, err)
					}
					return
				}
				if code != 1 || named == "" || strings.Contains(named, "warning:") {
					t.Fatalf("%s: code=%d stderr=%q", action, code, stderr.String())
				}

			})
		}
	}
}
