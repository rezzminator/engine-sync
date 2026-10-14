package opencode

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

const openCodeBuiltinDenialsForReadBashAgent = "tools:\n" +
	"  apply_patch: false\n  edit: false\n  execute: false\n  glob: false\n  grep: false\n" +
	"  invalid: false\n  lsp: false\n  plan_exit: false\n  question: false\n  skill: false\n" +
	"  todowrite: false\n  webfetch: false\n  websearch: false\n  write: false\n"

const openCodeBuiltinDenialsForReadOnly = "tools:\n" +
	"  apply_patch: false\n  bash: false\n  edit: false\n  execute: false\n  glob: false\n  grep: false\n" +
	"  invalid: false\n  lsp: false\n  plan_exit: false\n  question: false\n  skill: false\n" +
	"  task: false\n  todowrite: false\n  webfetch: false\n  websearch: false\n  write: false\n"

func TestRenderOpenCodeToolsBlockDisablesToolsOutsideClaudeAllowList(t *testing.T) {
	got, _, err := renderOpenCodeToolsBlock("worker.md", "Read, Bash, Agent", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, denied := range []string{
		"apply_patch", "edit", "execute", "glob", "grep", "invalid", "lsp", "plan_exit", "question",
		"skill", "todowrite", "webfetch", "websearch", "write",
	} {
		if !strings.Contains(got, "  "+denied+": false\n") {
			t.Errorf("tools block did not disable %q:\n%s", denied, got)
		}
	}
	for _, allowed := range []string{"bash", "read", "task"} {
		if strings.Contains(got, "  "+allowed+": false\n") {
			t.Errorf("tools block disabled allowed tool %q:\n%s", allowed, got)
		}
	}
}

func TestRenderOpenCodeToolsBlockLetsWriteAndEditReachApplyPatch(t *testing.T) {
	for _, allowList := range []string{"Write", "Edit"} {
		got, _, err := renderOpenCodeToolsBlock("worker.md", allowList, nil)
		if err != nil {
			t.Fatalf("render %s: %v", allowList, err)
		}
		if strings.Contains(got, "  apply_patch: false\n") {
			t.Fatalf("%s did not allow OpenCode's model editing tool:\n%s", allowList, got)
		}
	}
}

func TestRenderOpenCodeToolsBlockWithoutMCPEntriesDeniesEveryKnownServer(t *testing.T) {
	got, warnings, err := renderOpenCodeToolsBlock("worker.md", "Read, Bash, Agent", []string{"local", "chat-professor", "harvester-professor"})
	want := openCodeBuiltinDenialsForReadBashAgent + "  chat-professor_*: false\n  harvester-professor_*: false\n  local_*: false\n"
	if err != nil || got != want || len(warnings) != 0 {
		t.Fatalf("tools block = %q, %v, warnings %q; want %q", got, err, warnings, want)
	}
}

func TestRenderOpenCodeToolsBlockAllowsListedMCPTools(t *testing.T) {
	known := []string{"alpha", "chat-professor", "harvester-professor", "my_server"}
	for _, tc := range []struct {
		name      string
		allowList string
		mcpLines  string
	}{
		{
			name:      "one server",
			allowList: "Read, mcp__harvester-professor__read, mcp__harvester-professor__search_web",
			mcpLines: "  alpha_*: false\n  chat-professor_*: false\n" +
				"  harvester-professor_*: false\n" +
				"  harvester-professor_read: true\n" +
				"  harvester-professor_search_web: true\n" +
				"  my_server_*: false\n",
		},
		{
			name:      "family servers sorted, ids sorted and de-duplicated",
			allowList: "Read, mcp__harvester-professor__search_web, mcp__alpha__zeta, mcp__chat-professor__call, mcp__harvester-professor__read, mcp__alpha__beta, mcp__alpha__zeta",
			mcpLines: "  alpha_*: false\n" +
				"  alpha_beta: true\n" +
				"  alpha_zeta: true\n" +
				"  chat-professor_*: false\n" +
				"  chat-professor_call: true\n" +
				"  harvester-professor_*: false\n" +
				"  harvester-professor_read: true\n" +
				"  harvester-professor_search_web: true\n" +
				"  my_server_*: false\n",
		},
		{
			name:      "sanitized server keeps its underscores",
			allowList: "Read, mcp__my.server__do-it",
			mcpLines:  "  alpha_*: false\n  chat-professor_*: false\n  harvester-professor_*: false\n  my_server_*: false\n  my_server_do-it: true\n",
		},
		{
			name:      "server wildcard grants the whole server",
			allowList: "Read, mcp__harvester-professor__*, mcp__harvester-professor__read",
			mcpLines:  "  alpha_*: false\n  chat-professor_*: false\n  harvester-professor_*: true\n  my_server_*: false\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings, err := renderOpenCodeToolsBlock("worker.md", tc.allowList, known)
			if err != nil || got != openCodeBuiltinDenialsForReadOnly+tc.mcpLines || len(warnings) != 0 {
				t.Fatalf(
					"tools block = %q, %v, warnings %q; want %q",
					got,
					err,
					warnings,
					openCodeBuiltinDenialsForReadOnly+tc.mcpLines,
				)
			}
		})
	}
}

func TestRenderOpenCodeToolsBlockWarnsOnToolsItCannotMap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		allowList string
		warning   string
	}{
		{
			name:      "retired server prefixes a bare tool",
			allowList: "Read, mcp__archive__read",
			warning:   "retired MCP tool mcp__archive__read in worker.md — the hub server serves it as mcp__hub__archive_read",
		},
		{
			name:      "retired server keeps a prefixed tool",
			allowList: "Read, mcp__legacy__legacy_ship",
			warning:   "retired MCP tool mcp__legacy__legacy_ship in worker.md — the hub server serves it as mcp__hub__legacy_ship",
		},
		{
			name:      "unknown server",
			allowList: "Read, mcp__nosuch__x",
			warning:   "unknown MCP server in tool mcp__nosuch__x in worker.md — no configured or project .mcp.json server serves it; it stays denied",
		},
		{
			name:      "unmapped Claude tool",
			allowList: "Read, NotebookEdit",
			warning:   "unmapped Claude tool NotebookEdit in worker.md — no OpenCode equivalent; it stays denied",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings, err := renderOpenCodeToolsBlock("worker.md", tc.allowList, []string{"hub"}, config.MCP{Renamed: map[string]string{"legacy": "hub", "archive": "hub"}})
			want := openCodeBuiltinDenialsForReadOnly + "  hub_*: false\n"
			if err != nil || got != want {
				t.Fatalf("tools block = %q, %v; want %q", got, err, want)
			}
			if len(warnings) != 1 || warnings[0] != tc.warning {
				t.Fatalf("warnings = %q, want [%q]", warnings, tc.warning)
			}
		})
	}
}

func TestRenderOpenCodeToolsBlockKeepsARetiredNameThatIsAProjectServer(t *testing.T) {
	got, warnings, err := renderOpenCodeToolsBlock(
		"worker.md",
		"Read, mcp__archive__read",
		[]string{"archive", "hub"},
		config.MCP{Renamed: map[string]string{"archive": "hub"}},
	)
	if err != nil || len(warnings) != 0 || !strings.Contains(got, "  archive_*: false\n  archive_read: true\n") {
		t.Fatalf("tools block = %q, %v, warnings %q; want the project archive server granted", got, err, warnings)
	}
}

func TestCompileFailsOnMalformedMCPToolNamingAgentAndEntry(t *testing.T) {
	root := testjail.Scratch(t)
	agent := filepath.Join(root, ".claude", "agents", "broken.md")
	writeOpenCodeFixture(t, agent, "---\ndescription: Broken role.\ntools: Read, mcp__broken\n---\nWork.\n")

	result, err := Compile(Options{Root: root, Home: filepath.Join(root, "home"), Mode: ModeCheck})
	if err == nil && result.OK {
		t.Fatalf("compile succeeded with a malformed MCP tool: %#v", result)
	}
	report := strings.Join(result.Problems, "\n")
	if err != nil {
		report += "\n" + err.Error()
	}
	if !strings.Contains(report, agent) || !strings.Contains(report, `"mcp__broken"`) {
		t.Fatalf("compile report does not name %s and mcp__broken: %s", agent, report)
	}
}
