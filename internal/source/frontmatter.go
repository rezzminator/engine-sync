package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rezzminator/engine-sync/internal/config"
	"go.yaml.in/yaml/v3"
)

// Frontmatter is one source file's YAML header, parsed once by a real YAML
// parser for every source kind (agents, commands, skills, rules). Fields is
// the scalar view the emitters read: a top-level scalar as its text, a list of
// scalars joined by ", ", a nested map absent. Node keeps the parsed mapping,
// comments and key order included, for re-rendering a derived agent.
type Frontmatter struct {
	Description, Model, Effort string
	Tools                      []string
	Fields                     map[string]string
	Node                       *yaml.Node
	Requires                   []string
	Engines                    Engines
}

// Engines is an agent's per-engine block. A nil engine pointer is an absent
// block; Off is `engines.{engine}: false`, the agent is not emitted there.
type Engines struct {
	Codex    *CodexEngine
	OpenCode *OpenCodeEngine
}

type CodexEngine struct {
	Off                    bool
	Model, Effort, Sandbox string
}

type OpenCodeEngine struct {
	Off         bool
	Model, Mode string
	Permission  *config.Permission
}

// CodexOff and OpenCodeOff report `engines.{engine}: false`.
func (e Engines) CodexOff() bool    { return e.Codex != nil && e.Codex.Off }
func (e Engines) OpenCodeOff() bool { return e.OpenCode != nil && e.OpenCode.Off }

// InvalidError is a YAML header that does not parse: the artifact is skipped,
// never read as body.
type InvalidError struct {
	Line int
	Msg  string
}

func (e *InvalidError) Error() string { return fmt.Sprintf("line %d — %s", e.Line, e.Msg) }

// DescribeParseError names a parse failure for one file:
// `FRONTMATTER-INVALID {file}:{line} — {error}` for a YAML error, else
// `parse {file}: {error}`.
func DescribeParseError(path string, err error) string {
	var invalid *InvalidError
	if errors.As(err, &invalid) {
		return fmt.Sprintf("FRONTMATTER-INVALID %s:%d — %s", path, invalid.Line, invalid.Msg)
	}
	return fmt.Sprintf("parse %s: %v", path, err)
}

var yamlErrorLine = regexp.MustCompile(`^yaml: line (\d+): `)

// Split returns the header text between the fences, the body after the
// closing fence, and whether the text has a header at all.
func Split(text string) (header, body string, found bool, err error) {
	if !utf8.ValidString(text) {
		return "", "", false, fmt.Errorf("source is not UTF-8")
	}
	if strings.IndexByte(text, 0) >= 0 {
		return "", "", false, fmt.Errorf("source contains NUL")
	}
	lines := strings.Split(normalizeFrontmatter(text), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", text, false, nil
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), true, nil
		}
	}
	return "", "", false, &InvalidError{Line: 1, Msg: "frontmatter starts with --- but has no closing fence"}
}

// ParseHeader parses header YAML into its top-level mapping node. An empty
// header is an empty mapping. A header strict YAML refuses is retried once
// through compatibleHeader; one that still fails keeps the strict error.
func ParseHeader(header string) (*yaml.Node, error) {
	root, err := strictHeader(header)
	if err != nil {
		if retried, changed := compatibleHeader(header); changed {
			if node, retryErr := strictHeader(retried); retryErr == nil {
				return node, nil
			}
		}
	}
	return root, err
}

func strictHeader(header string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(header), &doc); err != nil {
		msg, line := err.Error(), 1
		if m := yamlErrorLine.FindStringSubmatch(msg); m != nil {
			line, _ = strconv.Atoi(m[1])
			msg = strings.TrimPrefix(msg, m[0])
		} else {
			msg = strings.TrimPrefix(msg, "yaml: ")
		}
		return nil, &InvalidError{Line: line + 1, Msg: msg}
	}
	if len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, &InvalidError{Line: root.Line + 1, Msg: "frontmatter is not a mapping"}
	}
	return root, nil
}

func Parse(text string) (Frontmatter, string, error) {
	front := Frontmatter{Fields: map[string]string{}}
	header, body, found, err := Split(text)
	if err != nil {
		return Frontmatter{}, "", err
	}
	if !found {
		return front, body, nil
	}
	node, err := ParseHeader(header)
	if err != nil {
		return Frontmatter{}, "", err
	}
	front = FromNode(node)
	return front, body, nil
}

// FromNode derives the scalar view, requires and engines from a header node.
// It never fails: ValidateAgent names every shape problem.
func FromNode(node *yaml.Node) Frontmatter {
	front := Frontmatter{Fields: map[string]string{}, Node: node}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if text, ok := scalarText(value); ok {
			front.Fields[key] = text
		}
	}
	front.Description, front.Model, front.Effort = front.Fields["description"], front.Fields["model"], front.Fields["effort"]
	for _, tool := range strings.Split(front.Fields["tools"], ",") {
		if tool = strings.TrimSpace(tool); tool != "" {
			front.Tools = append(front.Tools, tool)
		}
	}
	if requires := MapValue(node, "requires"); requires != nil && requires.Kind == yaml.SequenceNode {
		for _, item := range requires.Content {
			front.Requires = append(front.Requires, strings.TrimSpace(item.Value))
		}
	}
	front.Engines, _ = parseEngines(MapValue(node, "engines"))
	return front
}

// scalarText is a value's text in the scalar view: a block scalar loses its
// final line break, a list of scalars joins with ", ".
func scalarText(value *yaml.Node) (string, bool) {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Tag == "!!null" {
			return "", true
		}
		if value.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			return strings.TrimRight(value.Value, "\n"), true
		}
		return value.Value, true
	case yaml.SequenceNode:
		items := make([]string, 0, len(value.Content))
		for _, item := range value.Content {
			if item.Kind != yaml.ScalarNode {
				return "", false
			}
			items = append(items, item.Value)
		}
		return strings.Join(items, ", "), true
	}
	return "", false
}

// MapValue is the value node of key in a mapping node, or nil.
func MapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// Unmet is the first required capability the host's set lacks, or "". An
// empty name is never met: it is named as its quoted form.
func Unmet(requires, capabilities []string) string {
	for _, name := range requires {
		if name == "" {
			return `""`
		}
		if !slices.Contains(capabilities, name) {
			return name
		}
	}
	return ""
}

// Skipped is the line check and doctor print for an agent whose requires
// the host does not meet.
func Skipped(agent, requirement string) string {
	return fmt.Sprintf("SKIPPED %s — requires %s", agent, requirement)
}

// ClaudeNativeKeys are the Claude Code sub-agent frontmatter keys
// (code.claude.com/docs/en/sub-agents, "Supported frontmatter fields").
var ClaudeNativeKeys = []string{
	"name", "description", "tools", "disallowedTools", "model", "permissionMode",
	"maxTurns", "skills", "mcpServers", "hooks", "memory", "background",
	"omitClaudeMd", "effort", "isolation", "color", "initialPrompt", "experimental",
}

// OwnKeys are engine-sync's own agent keys; role is stamped by the compiler.
var OwnKeys = []string{"engines", "requires", "role"}

// RetiredKeys maps each retired flat engine key to its engines-block home.
var RetiredKeys = map[string]string{
	"codex-model":    "engines.codex.model",
	"codex-effort":   "engines.codex.effort",
	"codex-sandbox":  "engines.codex.sandbox",
	"opencode-model": "engines.opencode.model",
	"opencode-mode":  "engines.opencode.mode",
}

var (
	codexSettings    = []string{"model", "effort", "sandbox"}
	openCodeSettings = []string{"model", "mode", "permission"}
	agentEfforts     = []string{"low", "medium", "high", "xhigh", "max"}
	codexSandboxes   = []string{"inherit", "read-only", "workspace-write"}
	openCodeModes    = []string{"all", "primary", "subagent"}
)

// ValidateAgent checks one agent's frontmatter against the agent contract:
// problems fail the agent, warnings (UNKNOWN-KEY) do not. generated admits
// the compiler-stamped role.
func ValidateAgent(path string, front Frontmatter, passthrough []string, generated bool) (problems, warnings []string) {
	node := front.Node
	if node == nil {
		return nil, nil
	}
	if machineControls(node) {
		return []string{fmt.Sprintf("%s: declarations are machine scope only and must be compiled before loading", path)}, nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		switch {
		case slices.Contains(DeclarationKeys, key):
			problems = append(problems, fmt.Sprintf("%s: %s is machine scope only and must be compiled before loading", path, key))
		case RetiredKeys[key] != "":
			problems = append(problems, fmt.Sprintf("%s: %s moved to %s", path, key, RetiredKeys[key]))
		case key == "role" && !generated:
			problems = append(problems, fmt.Sprintf("%s: role is stamped by the compiler, never written in a source", path))
		case key == "requires":
			if value.Kind != yaml.SequenceNode {
				problems = append(problems, fmt.Sprintf("%s: requires must be a list of capability names", path))
				continue
			}
			for _, item := range value.Content {
				if item.Kind != yaml.ScalarNode || strings.TrimSpace(item.Value) == "" {
					problems = append(problems, fmt.Sprintf("%s: requires must be a list of capability names", path))
					break
				}
			}
		case key == "engines":
			if _, errs := parseEngines(value); len(errs) > 0 {
				for _, e := range errs {
					problems = append(problems, fmt.Sprintf("%s: %s", path, e))
				}
			}
		case key == "effort":
			if !slices.Contains(agentEfforts, value.Value) {
				problems = append(problems, fmt.Sprintf("%s: invalid effort %q (want %s)", path, value.Value, strings.Join(agentEfforts, ", ")))
			}
		case slices.Contains(ClaudeNativeKeys, key), slices.Contains(OwnKeys, key), slices.Contains(passthrough, key):
		default:
			warnings = append(warnings, fmt.Sprintf("UNKNOWN-KEY %s: %s", path, key))
		}
	}
	return problems, warnings
}

// parseEngines reads an engines block; errs names every shape problem.
func parseEngines(node *yaml.Node) (Engines, []string) {
	var engines Engines
	var errs []string
	if node == nil {
		return engines, nil
	}
	if node.Kind != yaml.MappingNode {
		return engines, []string{"engines must be a map of engine to settings"}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name, value := node.Content[i].Value, node.Content[i+1]
		off := value.Kind == yaml.ScalarNode && value.Tag == "!!bool" && value.Value == "false"
		if !off && value.Kind != yaml.MappingNode && name != "claude" {
			errs = append(errs, fmt.Sprintf("engines.%s must be a map of settings or false", name))
			continue
		}
		settings := map[string]*yaml.Node{}
		for j := 0; value.Kind == yaml.MappingNode && j+1 < len(value.Content); j += 2 {
			settings[value.Content[j].Value] = value.Content[j+1]
		}
		keys := make([]string, 0, len(settings))
		for key := range settings {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		text := func(key string, allowed []string) string {
			v := settings[key]
			if v == nil {
				return ""
			}
			if v.Kind != yaml.ScalarNode || strings.TrimSpace(v.Value) == "" {
				errs = append(errs, fmt.Sprintf("engines.%s.%s must be a nonempty string", name, key))
				return ""
			}
			if allowed != nil && !slices.Contains(allowed, v.Value) {
				errs = append(errs, fmt.Sprintf("engines.%s.%s: invalid value %q (want %s)", name, key, v.Value, strings.Join(allowed, ", ")))
				return ""
			}
			return strings.TrimSpace(v.Value)
		}
		switch name {
		case "claude":
			errs = append(errs, "engines.claude: Claude is the source engine; set its keys at the top level")
		case "codex":
			engines.Codex = &CodexEngine{Off: off}
			for _, key := range keys {
				if !slices.Contains(codexSettings, key) {
					errs = append(errs, fmt.Sprintf("engines.codex.%s: unknown setting (want %s)", key, strings.Join(codexSettings, ", ")))
				}
			}
			engines.Codex.Model, engines.Codex.Effort, engines.Codex.Sandbox = text("model", nil), text("effort", agentEfforts), text("sandbox", codexSandboxes)
		case "opencode":
			engines.OpenCode = &OpenCodeEngine{Off: off}
			for _, key := range keys {
				if !slices.Contains(openCodeSettings, key) {
					errs = append(errs, fmt.Sprintf("engines.opencode.%s: unknown setting (want %s)", key, strings.Join(openCodeSettings, ", ")))
				}
			}
			engines.OpenCode.Model, engines.OpenCode.Mode = text("model", nil), text("mode", openCodeModes)
			if node := settings["permission"]; node != nil {
				permission, err := decodePermission(node)
				if err != nil {
					errs = append(errs, fmt.Sprintf("engines.opencode.permission: %v", err))
				} else {
					engines.OpenCode.Permission = &permission
				}
			}
		default:
			errs = append(errs, fmt.Sprintf("engines.%s: unknown engine (want codex, opencode)", name))
		}
	}
	return engines, errs
}

// decodePermission reads an OpenCode permission value through the same
// ordered JSON decoder the configuration uses.
func decodePermission(node *yaml.Node) (config.Permission, error) {
	raw, err := NodeJSON(node)
	if err != nil {
		return config.Permission{}, err
	}
	var permission config.Permission
	if err := json.Unmarshal(raw, &permission); err != nil {
		return config.Permission{}, err
	}
	return permission, nil
}

// NodeJSON renders a YAML node as JSON, keeping mapping key order.
func NodeJSON(node *yaml.Node) ([]byte, error) {
	var out bytes.Buffer
	var write func(*yaml.Node) error
	write = func(n *yaml.Node) error {
		switch n.Kind {
		case yaml.DocumentNode:
			if len(n.Content) == 0 {
				out.WriteString("null")
				return nil
			}
			return write(n.Content[0])
		case yaml.AliasNode:
			return write(n.Alias)
		case yaml.MappingNode:
			out.WriteByte('{')
			for i := 0; i+1 < len(n.Content); i += 2 {
				if i > 0 {
					out.WriteByte(',')
				}
				key, _ := json.Marshal(n.Content[i].Value)
				out.Write(key)
				out.WriteByte(':')
				if err := write(n.Content[i+1]); err != nil {
					return err
				}
			}
			out.WriteByte('}')
		case yaml.SequenceNode:
			out.WriteByte('[')
			for i, item := range n.Content {
				if i > 0 {
					out.WriteByte(',')
				}
				if err := write(item); err != nil {
					return err
				}
			}
			out.WriteByte(']')
		default:
			var value any
			if err := n.Decode(&value); err != nil {
				return err
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			out.Write(raw)
		}
		return nil
	}
	if err := write(node); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func UnquoteScalar(value string) (string, error) {
	if len(value) < 2 || value[0] != value[len(value)-1] {
		return value, nil
	}
	switch value[0] {
	case '\'':
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), nil
	case '"':
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("decode double-quoted YAML scalar: %w", err)
		}
		return unquoted, nil
	default:
		return value, nil
	}
}

// HeaderLines is a header's raw lines but `name:`, for an emitter that
// carries a command's header through verbatim.
func HeaderLines(text string) []string {
	header, _, found, err := Split(text)
	if err != nil || !found {
		return nil
	}
	var out []string
	for _, line := range strings.Split(header, "\n") {
		if !strings.HasPrefix(line, "name:") {
			out = append(out, line)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

var frontmatterField = regexp.MustCompile(`^([A-Za-z-]+):\s*(.*)$`)

// ScalarField matches one `key: value` header line for a line-level rewrite.
func ScalarField(line string) []string { return frontmatterField.FindStringSubmatch(line) }

func normalizeFrontmatter(text string) string {
	return strings.ReplaceAll(strings.TrimPrefix(text, "\ufeff"), "\r\n", "\n")
}
