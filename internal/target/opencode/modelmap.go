package opencode

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/engine"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"sort"
	"strings"
)

const (
	openCodeBuildConfigPath = ".claude/opencode-build.json"
	openCodeAgentMode       = "all"
	openCodeApplyPatchTool  = "apply_patch"
	openCodeBashTool        = "bash"
	openCodeEditTool        = "edit"
	openCodeTaskTool        = "task"
	openCodeTodoWriteTool   = "todowrite"
)

var claudeToOpenCodeTools = map[string][]string{
	"Agent":           {openCodeTaskTool},
	"AskUserQuestion": {"question"},
	"Bash":            {openCodeBashTool},
	"Edit":            {openCodeEditTool, openCodeApplyPatchTool},
	"Glob":            {"glob"},
	"Grep":            {"grep"},
	"LSP":             {"lsp"},
	"Read":            {"read"},
	"SendMessage":     {openCodeTaskTool},
	"Skill":           {"skill"},
	"TodoRead":        {openCodeTodoWriteTool},
	"TodoWrite":       {openCodeTodoWriteTool},
	"WebFetch":        {"webfetch"},
	"WebSearch":       {"websearch"},
	"Write":           {"write", openCodeApplyPatchTool},
}

var openCodeToolIDs = []string{
	openCodeApplyPatchTool,
	openCodeBashTool,
	openCodeEditTool,
	"execute",
	"glob",
	"grep",
	"invalid",
	"lsp",
	"plan_exit",
	"question",
	"read",
	"skill",
	openCodeTaskTool,
	openCodeTodoWriteTool,
	"webfetch",
	"websearch",
	"write",
}

// knownMCPServers combines configured machine servers and project servers,
// each in OpenCode's tool-part charset (a server name is a frontmatter key, so
// no configured name reaches an agent file raw). A nil model is the
// machine-global path: no project .mcp.json sits beside a machine-global
// agent, so only the configured servers are known.
func knownMCPServers(model *claudesource.Project, root string, policies ...config.MCP) []string {
	known := map[string]bool{}
	for _, server := range selectedMCP(policies).GlobalServers {
		if name, _, err := engine.OpenCodeMCPToolParts("mcp__" + server + "__tool"); err == nil {
			known[name] = true
		}
	}
	var servers []claudesource.MCPServer
	var err error
	if model != nil {
		servers, _, err = model.Servers(filepath.Join(root, ".mcp.json"))
	}
	if err == nil {
		for _, server := range servers {
			if name, _, err := engine.OpenCodeMCPToolParts("mcp__" + server.Name + "__tool"); err == nil {
				known[name] = true
			}
		}
	}
	names := make([]string, 0, len(known))
	for name := range known {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// renderOpenCodeToolsBlock projects a Claude tools allow-list onto OpenCode:
// every builtin outside the list is denied, every known MCP server is denied
// unless a mcp__<server>__* wildcard grants it, and each listed tool of a known
// server is granted after its server's deny. A tool it cannot map — a retired
// or unknown server, a Claude tool with no OpenCode equivalent — stays denied
// and is named in a warning against source.
func renderOpenCodeToolsBlock(source, allowList string, known []string, policies ...config.MCP) (string, []string, error) {
	policy := selectedMCP(policies)
	knownServer := map[string]bool{}
	for _, server := range known {
		knownServer[server] = true
	}
	var warnings []string
	allowed := map[string]bool{}
	mcpAllowed := map[string]map[string]bool{}
	wildcard := map[string]bool{}
	unmapped := map[string]bool{}
	for _, claudeTool := range strings.Split(allowList, ",") {
		claudeTool = strings.TrimSpace(claudeTool)
		if claudeTool == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(claudeTool), "mcp__") {
			openCodeTools, ok := claudeToOpenCodeTools[claudeTool]
			if !ok {
				warnings = append(warnings, fmt.Sprintf(
					"unmapped Claude tool %s in %s — no OpenCode equivalent; it stays denied", claudeTool, source))
			}
			for _, openCodeTool := range openCodeTools {
				allowed[openCodeTool] = true
			}
			continue
		}
		isWildcard := strings.HasSuffix(claudeTool, "__*")
		parseName := claudeTool
		if isWildcard {
			parseName = strings.TrimSuffix(claudeTool, "*") + "all"
		}
		server, tool, err := engine.OpenCodeMCPToolParts(parseName)
		if err != nil {
			return "", nil, err
		}
		if !knownServer[server] {
			unmapped[server] = true
			if replacementServer := policy.Renamed[server]; replacementServer != "" {
				replacement := tool
				if isWildcard {
					replacement = "*"
				}
				if !strings.HasPrefix(replacement, server+"_") {
					replacement = server + "_" + replacement
				}
				warnings = append(warnings, fmt.Sprintf(
					"retired MCP tool %s in %s — the %s server serves it as mcp__%s__%s",
					claudeTool, source, replacementServer, replacementServer, replacement))
			} else {
				warnings = append(warnings, fmt.Sprintf(
					"unknown MCP server in tool %s in %s — no configured or project .mcp.json server serves it; it stays denied",
					claudeTool,
					source,
				))
			}
			continue
		}
		if isWildcard {
			wildcard[server] = true
			continue
		}
		if mcpAllowed[server] == nil {
			mcpAllowed[server] = map[string]bool{}
		}
		mcpAllowed[server][server+"_"+tool] = true
	}
	denied := make([]string, 0, len(openCodeToolIDs))
	for _, tool := range openCodeToolIDs {
		if !allowed[tool] {
			denied = append(denied, tool)
		}
	}
	sort.Strings(denied)
	var content strings.Builder
	content.WriteString("tools:\n")
	for _, tool := range denied {
		fmt.Fprintf(&content, "  %s: false\n", tool)
	}
	// A retired or unknown server is denied outright: OpenCode enables a tool
	// its map leaves out, so leaving the server unnamed would not deny it.
	servers := append([]string(nil), known...)
	for server := range unmapped {
		servers = append(servers, server)
	}
	sort.Strings(servers)
	for _, server := range servers {
		if wildcard[server] {
			fmt.Fprintf(&content, "  %s_*: true\n", server)
			continue
		}
		fmt.Fprintf(&content, "  %s_*: false\n", server)
		ids := make([]string, 0, len(mcpAllowed[server]))
		for id := range mcpAllowed[server] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Fprintf(&content, "  %s: true\n", id)
		}
	}
	return content.String(), warnings, nil
}

func selectedMCP(policies []config.MCP) config.MCP {
	if len(policies) > 0 {
		return policies[0]
	}
	return config.MCP{}
}
