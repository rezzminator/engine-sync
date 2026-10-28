package codex

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
	claudesource "github.com/rezzminator/engine-sync/internal/source"

	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	mcpBegin       = artifact.MCPBegin
	legacyMCPBegin = artifact.LegacyMCPScript
	mcpEnd         = artifact.MCPEnd
)

type mcpResult struct {
	Content  string
	Notes    []string
	Problems []string
	Changed  bool
}

func renderMCP(root string, model *claudesource.Project, current string) (mcpResult, error) {
	configPath := filepath.Join(root, ".codex", "config.toml")
	mcpPath := filepath.Join(root, ".mcp.json")
	hand := stripMCPFence(current)
	result := mcpResult{Content: hand, Problems: mcpFenceProblems(current, configPath)}
	parsed, present, err := model.Servers(mcpPath)
	if !present && err == nil {
		result.Changed = result.Content != current
		return result, nil
	}
	if err != nil {
		return result, err
	}
	servers, unmapped := codexMCPServers(parsed)
	for name, fields := range unmapped {
		if len(fields) != 0 {
			result.Notes = append(
				result.Notes,
				fmt.Sprintf("mcp %s: fields not mapped to codex config: %s", name, strings.Join(fields, ", ")),
			)
		}
	}
	sort.Strings(result.Notes)
	if len(servers) == 0 {
		result.Changed = result.Content != current
		return result, nil
	}
	handNames := map[string]bool{}
	re := regexp.MustCompile(`(?m)^\[mcp_servers\.([^\]]+)\]`)
	for _, match := range re.FindAllStringSubmatch(hand, -1) {
		handNames[strings.Trim(match[1], `"`)] = true
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	blocks := make([]string, 0, len(names))
	for _, name := range names {
		if handNames[name] {
			result.Problems = append(
				result.Problems,
				fmt.Sprintf(
					string(reconciler.Conflict)+" %s — [mcp_servers.%s] is hand-defined outside the generated fence AND in .mcp.json",
					configPath,
					name,
				),
			)
			continue
		}
		server := servers[name]
		lines := []string{"[mcp_servers." + tomlKey(name) + "]"}
		if server.Command != "" {
			lines = append(lines, "command = "+tomlString(server.Command))
		}
		if len(server.Args) > 0 {
			values := make([]string, len(server.Args))
			for i, arg := range server.Args {
				values[i] = tomlString(arg)
			}
			lines = append(lines, "args = ["+strings.Join(values, ", ")+"]")
		}
		if server.URL != "" {
			lines = append(lines, "url = "+tomlString(server.URL))
		}
		if len(server.Env) > 0 {
			keys := make([]string, 0, len(server.Env))
			for key := range server.Env {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			values := make([]string, len(keys))
			for i, key := range keys {
				values[i] = tomlKey(key) + " = " + tomlString(server.Env[key])
			}
			lines = append(lines, "env = { "+strings.Join(values, ", ")+" }")
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	if len(blocks) > 0 {
		result.Content = strings.TrimRight(
			hand,
			"\n",
		) + "\n\n" + mcpBegin + "\n" + strings.Join(
			blocks,
			"\n\n",
		) + "\n" + mcpEnd + "\n"
	}
	result.Changed = result.Content != current
	return result, nil
}

// mcpFenceProblems scans the untouched `.codex/config.toml` content for a
// malformed mcp_servers fence: an unpaired BEGIN (no matching END), a lone
// END (no preceding BEGIN), or more than one complete fence. stripMCPFence's
// regexp only ever matches a well-formed BEGIN...END pair, so any of these
// shapes previously passed through untouched as "hand" content and a second
// fence got appended beside it with no Problem raised in either build or
// check (L3-F11). Each is now a named Problem; compileMCP's caller already
// skips writing .codex/config.toml whenever Problems is non-empty, so build
// never appends a second fence over a malformed one.
func mcpFenceProblems(current, configPath string) []string {
	var problems []string
	inFence := false
	fences := 0
	for _, line := range strings.Split(current, "\n") {
		switch line {
		case mcpBegin, artifact.PriorMCP, legacyMCPBegin:
			if inFence {
				problems = append(problems, fmt.Sprintf(
					string(reconciler.Conflict)+" %s: a second mcp_servers BEGIN fence starts before the first one's END — hand-fix or remove the stale fence",
					configPath,
				))
				continue
			}
			inFence = true
		case mcpEnd:
			if !inFence {
				problems = append(problems, fmt.Sprintf(
					string(reconciler.Conflict)+" %s: an mcp_servers END fence has no matching BEGIN — hand-fix or remove the stale marker",
					configPath,
				))
				continue
			}
			inFence = false
			fences++
		}
	}
	if inFence {
		problems = append(problems, fmt.Sprintf(
			string(reconciler.Conflict)+" %s: an mcp_servers BEGIN fence has no matching END — hand-fix or remove the stale marker",
			configPath,
		))
	}
	if fences > 1 {
		problems = append(problems, fmt.Sprintf(
			string(reconciler.Conflict)+" %s: %d mcp_servers fences found, want at most one",
			configPath,
			fences,
		))
	}
	return problems
}

func stripMCPFence(current string) string {
	for _, begin := range artifact.MCPBegins() {
		pattern := regexp.MustCompile(`(?s)\n?` + regexp.QuoteMeta(begin) + `.*?` + regexp.QuoteMeta(mcpEnd) + `\n?`)
		current = pattern.ReplaceAllString(current, "\n")
	}
	return current
}
