package config

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Schema is generated from the same tagged types accepted by the loader.
func Schema() []byte {
	p := schemaType(reflect.TypeFor[Config]())
	m := schemaType(reflect.TypeFor[MachineFile]())
	m["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	m["title"] = "engine-sync settings"
	m["$defs"] = map[string]any{"project": p}
	raw, _ := json.MarshalIndent(m, "", "  ")
	return append(raw, '\n')
}
func schemaType(t reflect.Type) map[string]any {
	if t == reflect.TypeFor[Permission]() {
		return map[string]any{"anyOf": []any{map[string]any{"enum": []string{"allow", "ask", "deny"}}, map[string]any{"type": "object", "additionalProperties": map[string]any{"anyOf": []any{map[string]any{"enum": []string{"allow", "ask", "deny"}}, map[string]any{"type": "object", "additionalProperties": map[string]any{"enum": []string{"allow", "ask", "deny"}}}}}}}}
	}
	switch t.Kind() {
	case reflect.Struct:
		props := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			key := strings.Split(f.Tag.Get("json"), ",")[0]
			if key == "-" {
				continue
			}
			props[key] = schemaType(f.Type)
		}
		out := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if _, ok := props["version"]; ok {
			out["required"] = []string{"version"}
			props["version"] = map[string]any{"const": 1}
		}
		return out
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaType(t.Elem())}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": schemaType(t.Elem())}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int:
		return map[string]any{"type": "integer"}
	default:
		return map[string]any{"type": "string"}
	}
}
func Effective(cfg Config) MachineFile {
	return MachineFile{Version: cfg.Version, Codex: cfg.Codex, OpenCode: cfg.OpenCode, MCP: cfg.MCP, WorkbenchMarker: cfg.WorkbenchMarker, Rewrites: cfg.Rewrites, Machine: cfg.Machine}
}
