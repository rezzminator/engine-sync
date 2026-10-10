package cli

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/testjail"
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
