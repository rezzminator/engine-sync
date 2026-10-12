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

func TestReviewProjectUsageAndFailures(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	putProjectFile(t, root, "CLAUDE.md", "Sample\n")
	putProjectFile(t, root, "AGENTS.md", "foreign")
	e := env{get: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}}
	for _, args := range [][]string{{"check", "--dry-run", root}, {"doctor", "--dry-run", root}, {"codex", "build", "--engine", "opencode", root}, {"opencode", "build", "--engine", "codex", root}} {
		var out, errOut bytes.Buffer
		if rc := dispatch(args, &out, &errOut, e); rc != 2 || errOut.Len() == 0 {
			t.Errorf("usage %v rc=%d: %s%s", args, rc, &out, &errOut)
		}
	}
	var out, errOut bytes.Buffer
	if rc := dispatch([]string{"plan", root}, &out, &errOut, e); rc != 1 || !strings.Contains(errOut.String(), "CONFLICT") {
		t.Errorf("silent plan rc=%d: %s%s", rc, &out, &errOut)
	}
}

func TestReviewMachinePolicyIndependence(t *testing.T) {
	home := testjail.Scratch(t)
	putProjectFile(t, home, ".claude/commands/hello.md", "---\ndescription: Hello.\n---\nTOKEN /hello.\n")
	e := env{get: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}}
	for _, engine := range []string{"codex", "opencode"} {
		for _, word := range []string{"FIRST", "SECOND"} {
			root := testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
			putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1,"rewrites":[{"literal":"TOKEN","replace":"`+word+`"}]}`)
			var out, errOut bytes.Buffer
			if rc := dispatch([]string{"build", root, "--engine", engine}, &out, &errOut, e); rc != 0 {
				t.Errorf("build rc=%d %s%s", rc, &out, &errOut)
			}
			path := filepath.Join(home, ".codex", "prompts", "hello.md")
			if engine == "opencode" {
				path = filepath.Join(home, ".config", "opencode", "command", "hello.md")
			}
			raw, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(raw), "TOKEN") || strings.Contains(string(raw), word) {
				t.Errorf("project changed machine bytes: %s %v", raw, err)
			}
		}
	}
}

func TestReviewRejectHomeAndCanonicalizeProject(t *testing.T) {
	home := testjail.Scratch(t)
	root := filepath.Join(home, "projects", "sample")
	putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
	e := env{get: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}}
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{home, filepath.Dir(home), alias} {
		var out, errOut bytes.Buffer
		rc := dispatch([]string{"build", path, "--engine", "codex"}, &out, &errOut, e)
		if path == alias {
			if rc != 0 {
				t.Errorf("alias build %d %s", rc, &errOut)
			}
			var cfgOut bytes.Buffer
			if c := dispatch([]string{"config", "validate", alias}, &cfgOut, &errOut, e); c != 0 || !strings.Contains(cfgOut.String(), root) {
				t.Errorf("root differs: %d %s", c, &cfgOut)
			}
		} else if rc == 0 || !strings.Contains(errOut.String(), "project root") {
			t.Errorf("unsafe root allowed rc=%d %s%s", rc, &out, &errOut)
		}
	}
}

func TestReviewSharedRoleNaming(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	for _, rel := range []string{"CLAUDE.md", "unit-api/CLAUDE.md"} {
		putProjectFile(t, root, rel, "# Sample\n")
	}
	// Each file base differs from its frontmatter name: the role is named by
	// the frontmatter, the suffix mode applied on top.
	putProjectFile(t, root, "unit-api/.claude/agents/reader-file.md", "---\nname: reader\ndescription: Reads.\n---\nRead.\n")
	putProjectFile(t, root, ".claude/agents/lead-file.md", "---\nname: lead\ndescription: Leads.\n---\nLead.\n")
	putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1,"codex":{"projects":["unit-api"],"suffixMode":"strip-prefix","suffixPrefix":"unit-"}}`)
	e := env{get: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}}
	var out, errOut bytes.Buffer
	if rc := dispatch([]string{"build", root}, &out, &errOut, e); rc != 0 {
		t.Fatalf("build %d %s%s", rc, &out, &errOut)
	}
	for rel, want := range map[string]string{
		".codex/agents/reader-api.toml": `name = "reader-api"`, ".opencode/agent/reader-api.md": "Read.",
		".codex/agents/lead.toml": `name = "lead"`, ".opencode/agent/lead.md": "Lead.",
	} {
		if raw, err := os.ReadFile(filepath.Join(root, rel)); err != nil || !strings.Contains(string(raw), want) {
			t.Errorf("shared naming differs: %s lacks %q (%v)", rel, want, err)
		}
	}
	for _, rel := range []string{".codex/agents/lead-file.toml", ".opencode/agent/reader-file-api.md"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			t.Errorf("%s named by its file base", rel)
		}
	}
}
