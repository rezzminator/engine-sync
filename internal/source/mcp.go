package source

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type MCPServer struct {
	Name    string                     `json:"-"`
	Command string                     `json:"command"`
	Args    []string                   `json:"args"`
	Env     map[string]string          `json:"env"`
	URL     string                     `json:"url"`
	Type    string                     `json:"type"`
	Headers map[string]string          `json:"headers"`
	Fields  map[string]json.RawMessage `json:"-"`
}

func DecodeMCP(raw []byte) ([]MCPServer, error) {
	var object struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("parse .mcp.json: %w", err)
	}
	names := make([]string, 0, len(object.Servers))
	for name := range object.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make([]MCPServer, 0, len(names))
	for _, name := range names {
		server := MCPServer{Name: name}
		body := object.Servers[name]
		if err := json.Unmarshal(body, &server); err != nil {
			return nil, fmt.Errorf("parse .mcp.json server %q: %w", name, err)
		}
		if err := json.Unmarshal(body, &server.Fields); err != nil {
			return nil, fmt.Errorf("parse .mcp.json server %q fields: %w", name, err)
		}
		servers = append(servers, server)
	}
	return servers, nil
}
func (p *Project) Servers(path string) ([]MCPServer, bool, error) {
	if p != nil && p.mcpPath == path {
		return p.MCP, p.mcpPresent, p.mcpErr
	}
	raw, err := p.Read(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	servers, err := DecodeMCP(raw)
	if p != nil {
		p.mcpPath, p.mcpPresent, p.MCP, p.mcpErr = path, true, servers, err
	}
	return servers, true, err
}
