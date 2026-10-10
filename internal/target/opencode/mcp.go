package opencode

import (
	claudesource "github.com/rezzminator/engine-sync/internal/source"
)

func compileOpenCodeMCP(path string, warn, problem func(string, ...any), models ...*claudesource.Project) (map[string]any, bool) {
	var model *claudesource.Project
	if len(models) > 0 {
		model = models[0]
	}
	servers, present, err := model.Servers(path)
	if err != nil {
		problem("parse %s: %v", path, err)
		return nil, present
	}
	if !present {
		return nil, false
	}
	result := map[string]any{}
	for _, server := range servers {
		name := server.Name
		switch {
		case server.Command != "":
			command := append([]string{server.Command}, server.Args...)
			entry := map[string]any{opencodeType: "local", "command": command, "enabled": true}
			if len(server.Env) != 0 {
				entry["environment"] = server.Env
			}
			result[name] = entry
		case server.URL != "":
			entry := map[string]any{opencodeType: "remote", "url": server.URL, "enabled": true}
			if len(server.Headers) != 0 {
				entry["headers"] = server.Headers
			}
			result[name] = entry
		default:
			warn("mcp %s: neither command nor url — skipped, never silently dropped from the config", name)
			continue
		}
		for key := range server.Fields {
			if key != "command" && key != "args" && key != "env" && key != "url" && key != opencodeType && key != "headers" {
				warn("mcp %s: fields not mapped to opencode config: %s", name, key)
			}
		}

	}
	return result, true
}
