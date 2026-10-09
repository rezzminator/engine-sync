// Package engine names the three engines engine-sync compiles between, in the
// spelling a human types on a command line, and holds the one rule that turns
// a Claude MCP tool name into OpenCode's.
package engine

import (
	"fmt"
	"strings"
)

// ID is one engine.
type ID string

// The three engines.
const (
	Claude   ID = "claude"
	Codex    ID = "codex"
	OpenCode ID = "opencode"
)

// Descriptor is what engine-sync knows about one engine.
type Descriptor struct {
	ID ID
	// LongName is the spelling a human types on the CLI ("claude").
	LongName string
}

var registry = map[ID]Descriptor{
	Claude:   {ID: Claude, LongName: "claude"},
	Codex:    {ID: Codex, LongName: "codex"},
	OpenCode: {ID: OpenCode, LongName: "opencode"},
}

// MustLookup returns the descriptor of a known engine and panics on an
// unknown one: every caller names a constant above.
func MustLookup(id ID) Descriptor {
	descriptor, ok := registry[id]
	if !ok {
		panic(fmt.Sprintf("engine: unknown engine %q", id))
	}
	return descriptor
}

// OpenCodeMCPToolParts joined by '_'. The caller checks the mcp__ prefix; this is
// the one home of the translation rule.
func OpenCodeMCPToolName(claudeTool string) (string, error) {
	serverName, toolName, err := OpenCodeMCPToolParts(claudeTool)
	if err != nil {
		return "", err
	}
	return serverName + "_" + toolName, nil
}

// OpenCodeMCPToolParts splits a Claude MCP tool name (mcp__server__tool) after
// its prefix on the first "__" and sanitizes each part to letters, digits, '_'
// and '-', as OpenCode names a server's tools.
func OpenCodeMCPToolParts(claudeTool string) (serverName, toolName string, err error) {
	parts := strings.SplitN(claudeTool[len("mcp__"):], "__", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("OpenCode MCP tool %q must be mcp__server__tool", claudeTool)
	}
	serverName = sanitizeOpenCodeToolPart(parts[0])
	toolName = sanitizeOpenCodeToolPart(parts[1])
	if serverName == "" || toolName == "" {
		return "", "", fmt.Errorf("OpenCode MCP tool %q has no usable server or tool name", claudeTool)
	}
	return serverName, toolName, nil
}

func sanitizeOpenCodeToolPart(value string) string {
	var result strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteByte('_')
		}
	}
	return result.String()
}
