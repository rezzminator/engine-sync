package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"sort"
	"strings"
)

const (
	opencodeAllow = "allow"
	opencodeDeny  = "deny"
	opencodeType  = "type"
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
				warnings = append(warnings, fmt.Sprintf("unparseable %s (%v) — regenerating from sources; adopter keys could not be preserved", path, err))
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
		artifact.Marker(artifact.JSONC, "engine-sync opencode build + .mcp.json") + "\n",
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
	var out bytes.Buffer
	inString, escaped := false, false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inString {
			out.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(raw) && raw[i+1] == '/' {
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			if i < len(raw) {
				out.WriteByte('\n')
			}
			continue
		}
		out.WriteByte(c)
	}
	return out.Bytes()
}
