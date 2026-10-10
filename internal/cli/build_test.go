package cli

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The public entry points share one neutral project policy, including hooks.
func TestStandaloneProject(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	files := map[string]string{
		"CLAUDE.md":                      "# Sample\nUse /hello.\n",
		".claude/agents/reader.md":       "---\nname: reader\ndescription: Reads files.\ntools: Read, Bash\nmodel: opus\n---\nRead the source.\n",
		".claude/commands/hello.md":      "---\ndescription: Say hello.\n---\nHello.\n",
		".claude/skills/sample/SKILL.md": "---\nname: sample\ndescription: Sample skill.\n---\nRead files.\n",
		".mcp.json":                      `{"mcpServers":{"sample":{"command":"sample-server"}}}`,
	}
	for name, content := range files {
		putProjectFile(t, root, name, content)
	}
	putProjectFile(t, home, ".claude/agents/reader.md", files[".claude/agents/reader.md"])
	vars := map[string]string{"HOME": home, "CLAUDE_PROJECT_DIR": root}
	e := env{get: func(key string) string { return vars[key] }}
	forbidden := []string{"Professor", "professor", "pfm", "gitter", "guarded files", "SECURITY.md", "LICENSE", "codex-build.json"}
	scan := func(label, content string) {
		for _, term := range forbidden {
			if strings.Contains(content, term) {
				t.Errorf("%s contains %q: %s", label, term, content)
			}
		}
	}
	for _, args := range [][]string{{"build", root}, {"check", root}, {"doctor", root}, {"global", "--plan"}, {"hook", "claude", "mark"}, {"hook", "claude", "stop"}, {"hook", "claude", "start"}} {
		var out, errOut bytes.Buffer
		e.stdin = strings.NewReader(`{"tool_input":{"file_path":` + strconvQuote(filepath.Join(root, "CLAUDE.md")) + `}}`)
		if code := dispatch(args, &out, &errOut, e); code != 0 {
			t.Fatalf("%v rc=%d: %s%s", args, code, &out, &errOut)
		}
		scan(strings.Join(args, " "), out.String()+errOut.String())
	}
	for _, dir := range []string{root, home} {
		if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type().IsRegular() {
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				scan(path, string(raw))
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCommandsIgnoreUnrelatedEngineHomeEntries(t *testing.T) {
	for _, entry := range []string{"socket", "unreadable"} {
		t.Run(entry, func(t *testing.T) {
			root, home := testjail.Scratch(t), testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
			vars := map[string]string{"HOME": home, "CLAUDE_PROJECT_DIR": root}
			e := env{get: func(key string) string { return vars[key] }}
			var out, errOut bytes.Buffer
			if code := dispatch([]string{"build", root}, &out, &errOut, e); code != 0 {
				t.Fatalf("fixture build rc=%d: %s%s", code, &out, &errOut)
			}
			path := filepath.Join(home, ".codex", "sessions", "x.jsonl")
			if entry == "socket" {
				path = filepath.Join(home, ".codex", "ipc", "ipc.sock")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				listener, err := net.Listen("unix", path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { listener.Close() })
			} else {
				putProjectFile(t, home, ".codex/sessions/x.jsonl", "private session\n")
				if err := os.Chmod(path, 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(path, 0600) })
				if _, err := os.ReadFile(path); err == nil {
					t.Skip("permission proof requires a non-root user")
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"codex", "check", root}, {"build", root}, {"opencode", "check", root}, {"hook", "claude", "stop"}} {
				name := args[0]
				if args[1] != root {
					name = strings.Join(args, " ")
					name = strings.TrimSuffix(name, " "+root)
				}
				t.Run(name, func(t *testing.T) {
					out.Reset()
					errOut.Reset()
					if args[0] == "hook" {
						setFlag(t, e, root)
					}
					if code := dispatch(args, &out, &errOut, e); code != 0 || (args[0] == "hook" && out.Len() != 0) {
						t.Errorf("%v rc=%d: %s%s", args, code, &out, &errOut)
					}
					after, err := os.Lstat(path)
					if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
						t.Errorf("unrelated entry changed: before=%v after=%v err=%v", before, after, err)
					}
				})
			}
		})
	}
}
