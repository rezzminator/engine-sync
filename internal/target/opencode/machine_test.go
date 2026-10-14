package opencode

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineCommandDestination(t *testing.T) {
	home := testjail.Scratch(t)
	writeOpenCodeFixture(t, filepath.Join(home, ".claude", "commands", "hello.md"), "---\ndescription: Hello.\n---\nHi.\n")
	cfg := config.Default()
	p := source.LoadMachineCommands(home, home, false, cfg)
	outputs, ds := renderOpenCodeMachine(p, cfg)
	if len(ds) != 0 || len(outputs) != 1 || outputs[0].Path != filepath.Join(home, ".config", "opencode", "command", "hello.md") {
		t.Fatalf("outputs=%#v diagnostics=%#v", outputs, ds)
	}
}

// A machine-global agent has no project .mcp.json beside it, so the servers
// the machine config names in mcp.globalServers are the ones it may grant; a
// server named nowhere stays denied and is named in a warning.
func TestMachineAgentGrantsConfiguredGlobalServersAndWarnsOnUnknownOnes(t *testing.T) {
	home := testjail.Scratch(t)
	agents := filepath.Join(home, "source", "agents")
	writeOpenCodeFixture(t, filepath.Join(agents, "worker.md"),
		"---\ndescription: Work.\ntools: Read, mcp__tools-hub__lookup, mcp__nosuch__x\n---\nWork.\n")
	cfg := config.Default()
	cfg.MCP.GlobalServers = []string{"tools-hub"}
	p, err := source.LoadMachine(home, agents, filepath.Join(home, "generated"))
	if err != nil {
		t.Fatal(err)
	}
	outputs, ds := renderOpenCodeMachineAgents(p, cfg)
	if len(outputs) != 1 {
		t.Fatalf("outputs=%#v diagnostics=%#v, want the one agent", outputs, ds)
	}
	raw := string(outputs[0].Content)
	if !strings.Contains(raw, "  tools-hub_*: false\n  tools-hub_lookup: true\n") {
		t.Errorf("agent %q does not grant mcp__tools-hub__lookup after its server's deny", raw)
	}
	if strings.Contains(raw, "nosuch") {
		t.Errorf("agent %q grants or names the unknown server", raw)
	}
	var warnings []string
	for _, d := range ds {
		warnings = append(warnings, d.Message)
	}
	joined := strings.Join(warnings, "\n")
	if strings.Contains(joined, "mcp__tools-hub__lookup") {
		t.Errorf("warnings %q name the configured server's tool", joined)
	}
	if !strings.Contains(joined, "unknown MCP server in tool mcp__nosuch__x") {
		t.Errorf("warnings %q do not name the unknown server's tool", joined)
	}
}
