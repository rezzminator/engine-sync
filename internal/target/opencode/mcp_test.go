package opencode

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeAgentWithoutMCPToolsDeniesEveryKnownServer(t *testing.T) {
	root := testjail.Scratch(t)
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"mcp":{"globalServers":["chat-professor","harvester-professor"]}}`)
	home := filepath.Join(root, "home")
	agent := filepath.Join(root, ".claude", "agents", "reader.md")
	writeOpenCodeFixture(
		t,
		agent,
		"---\ndescription: Reader role.\ntools:\n  - Read\n  - Grep\n  - NotebookEdit\n---\nRead.\n",
	)
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".mcp.json"),
		`{"mcpServers":{"local":{"command":"pfm","args":["mcp","serve"]},"my.remote":{"url":"http://127.0.0.1:9/mcp"}}}`,
	)

	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("build result=%#v err=%v", result, err)
	}
	compiled, _ := os.ReadFile(filepath.Join(root, ".opencode", "agent", "reader.md"))
	if !strings.Contains(
		string(compiled),
		"  write: false\n  chat-professor_*: false\n  harvester-professor_*: false\n  local_*: false\n  my_remote_*: false\n---\n",
	) ||
		strings.Contains(string(compiled), "  read: false\n") ||
		strings.Contains(string(compiled), "  grep: false\n") {
		t.Fatalf("agent without MCP tools did not deny every known server: %s", compiled)
	}
	want := "unmapped Claude tool NotebookEdit in " + agent + " — no OpenCode equivalent; it stays denied"
	if !containsProblem(result.Warnings, want) {
		t.Fatalf("warnings = %q, want %q", result.Warnings, want)
	}
}
