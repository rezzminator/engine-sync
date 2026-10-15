package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"sort"
	"strings"
)

const (
	opencodeDeny = "deny"
	opencodeType = "type"
)

func compileConfigWithSource(model *claudesource.Project, root string, add func(artifact.Artifact), problem, warn func(string, ...any), policies ...config.OpenCode) {
	path := filepath.Join(root, ".opencode", "opencode.jsonc")
	mcp, hasMCP := compileOpenCodeMCP(filepath.Join(root, ".mcp.json"), warn, problem, model)
	output := artifact.Artifact{Path: path, Content: []byte(serializeOpenCodeConfig(nil, mcp, hasMCP, policies...)), Source: filepath.Join(root, ".mcp.json"), Syntax: artifact.JSONC}
	output.Merge = func(raw []byte) ([]byte, []string, []string) {
		extra := map[string]json.RawMessage{}
		var warnings []string
		if len(raw) > 0 {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(parseOpenCodeJSONC(raw), &object); err != nil {
				return nil, nil, []string{fmt.Sprintf("%s %s (%v) — preserved; repair JSONC before rebuilding", reconcile.Unparseable, path, err)}
			}
			for key, value := range object {
				if key != "$schema" && key != "permission" && key != "mcp" {
					extra[key] = value
				}
			}
		}
		return []byte(serializeOpenCodeConfig(extra, mcp, hasMCP, policies...)), warnings, nil
	}
	add(output)
}

func serializeOpenCodeConfig(extra map[string]json.RawMessage, mcp map[string]any, hasMCP bool, policies ...config.OpenCode) string {
	var body bytes.Buffer
	body.WriteString(
		artifact.Marker(artifact.JSONC, "configuration + .mcp.json") + "\n",
	)
	body.WriteString("// Sources: this compiler owns $schema, permission, and mcp. Other keys survive regeneration.\n")
	body.WriteString("{\n  \"$schema\": \"https://opencode.ai/config.json\"")
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		encoded, _ := json.Marshal(extra[key])
		body.WriteString(",\n  " + quoteOpenCodeJSON(key) + ": " + indentOpenCodeJSON(encoded, "  "))
	}
	permission, _ := json.Marshal(selectedPolicy(policies).Permission)
	body.WriteString(",\n  \"permission\": " + indentOpenCodeJSON(permission, "  "))
	if hasMCP && len(mcp) != 0 {
		encoded, _ := json.Marshal(mcp)
		body.WriteString(",\n  \"mcp\": " + indentOpenCodeJSON(encoded, "  "))
	}
	body.WriteString("\n}\n")
	return body.String()
}

func indentOpenCodeJSON(raw []byte, prefix string) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	lines := strings.Split(out.String(), "\n")
	if len(lines) == 1 {
		return lines[0]
	}
	for i := 1; i < len(lines); i++ {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func quoteOpenCodeJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func quoteOpenCodeYAML(value string) string { return quoteOpenCodeJSON(value) }

func parseOpenCodeJSONC(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	quoted, escaped := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quoted {
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(raw) {
			if raw[i+1] == '/' {
				for i < len(raw) && raw[i] != '\n' {
					i++
				}
				out = append(out, '\n')
				continue
			}
			if raw[i+1] == '*' {
				i += 2
				for i+1 < len(raw) && !(raw[i] == '*' && raw[i+1] == '/') {
					if raw[i] == '\n' {
						out = append(out, '\n')
					}
					i++
				}
				if i+1 == len(raw) {
					return raw
				}
				i++
				out = append(out, ' ')
				continue
			}
		}
		out = append(out, c)
	}
	quoted, escaped = false, false
	for i, c := range out {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(out) && (out[j] == ' ' || out[j] == '\n' || out[j] == '\r' || out[j] == '\t') {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				out[i] = ' '
			}
		}
	}
	return out
}
