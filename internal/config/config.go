package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/paths"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const RelPath = ".claude/engine-sync.json"

const LegacyRelPath = ".claude/codex-build.json"

type Config struct {
	ClaudeConfigDir string   `json:"-"`
	Version         int      `json:"version"`
	Codex           Codex    `json:"codex"`
	OpenCode        OpenCode `json:"opencode"`
	WorkbenchMarker string   `json:"workbenchMarker"`
	MCP             MCP      `json:"mcp"`
	Machine         Machine  `json:"-"`
	Targets         *Targets `json:"-"`
	Agents          Agents   `json:"agents"`
	// Capabilities is the host's capability set for this run: every
	// --capability flag plus the global file's targets.capabilities. It is never read
	// from a config file and carries no meaning beyond set membership.
	Capabilities []string          `json:"-"`
	Source       string            `json:"-"`
	Legacy       bool              `json:"-"`
	Sources      map[string]string `json:"-"`
	Deprecations []string          `json:"-"`
	Rewrites     []Rewrite         `json:"rewrites"`
	CommandDirs  []string          `json:"commandDirs"`
}

type Review struct {
	ModelAlias    string `json:"modelAlias"`
	DefaultEffort string `json:"defaultEffort"`
	Prompt        string `json:"prompt"`
	SlotPrompt    string `json:"slotPrompt"`
}

type Codex struct {
	Review          Review            `json:"review"`
	GlobalCommands  bool              `json:"globalCommands"`
	ModelMap        map[string]string `json:"modelMap"`
	RootAdapter     string            `json:"rootAdapter"`
	AgentPreamble   string            `json:"agentPreamble"`
	ExcludeDirs     []string          `json:"excludeDirs"`
	Projects        []string          `json:"projects"`
	ExcludeProjects []string          `json:"excludeProjects"`
	NeverRegister   []string          `json:"neverRegister"`
	SuffixMode      string            `json:"suffixMode"`
	SuffixPrefix    string            `json:"suffixPrefix"`
}

type OpenCode struct {
	ModelMap      map[string]string `json:"modelMap"`
	AgentPreamble string            `json:"agentPreamble"`
	Permission    Permission        `json:"permission"`
	MirrorFiles   []string          `json:"mirrorFiles"`
}

// Agents is the agent frontmatter policy: PassthroughKeys names extra
// top-level keys a host accepts beside the Claude Code native keys and
// engine-sync's own, so they never warn as UNKNOWN-KEY.
type Agents struct {
	PassthroughKeys []string `json:"passthroughKeys" pattern:"^[^ \\t\\n:]*[^\\t\\n\\v\\f\\r :\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000][^ \\t\\n:]*$"`
}

type MCP struct {
	GlobalServers []string          `json:"globalServers"`
	Renamed       map[string]string `json:"renamed"`
}

type Machine struct {
	SourceAgentsDir string    `json:"sourceAgentsDir"`
	AgentMarkerDir  string    `json:"agentMarkerDir"`
	ClaudeAgentsDir string    `json:"claudeAgentsDir"`
	RetiredAgents   []string  `json:"retiredAgents"`
	RetiredFiles    []string  `json:"retiredFiles"`
	Rewrites        []Rewrite `json:"rewrites"`
	CodexHomes      []string  `json:"codexHomes"`
	// SourceGeneratedAgentsDir is the directory of the source repository the
	// declarations that carry `from:` render into.
	SourceGeneratedAgentsDir string `json:"sourceGeneratedAgentsDir"`
	// ManagedBy names the installer that owns the registry files `global`
	// writes: it follows the generated header of each, so a later doctor
	// names a file whose source is gone.
	ManagedBy string `json:"managedBy,omitempty" pattern:"^[a-z][a-z0-9-]{0,31}$"`
}

type MachineFile struct {
	Version         int       `json:"version"`
	Codex           Codex     `json:"codex"`
	OpenCode        OpenCode  `json:"opencode"`
	WorkbenchMarker string    `json:"workbenchMarker"`
	MCP             MCP       `json:"mcp"`
	Rewrites        []Rewrite `json:"rewrites"`
	CommandDirs     []string  `json:"commandDirs"`
	Machine         Machine   `json:"machine"`
	Targets         *Targets  `json:"targets,omitempty"`
	Agents          Agents    `json:"agents"`
}

type Rewrite struct {
	Engines []string `json:"engines"`
	Kinds   []string `json:"kinds"`
	Literal string   `json:"literal"`
	Replace string   `json:"replace"`
}

const DefaultAgentPreamble = "You are the {{.Name}} role in this repository, running as a native {{.Engine}} subagent.\nRead the repository root {{.Root}} before starting your task.\n\n"

const DefaultRootAdapter = `
## {{.Engine}} adapter — reading this file in the {{.Engine}} harness

This file is compiled from {{.Source}} by engine-sync build. Translate harness mechanics to the matching {{.Engine}} command or role: edit the source and rebuild.
`

func defaultModelMap() map[string]string { return map[string]string{} }

func Default() Config {
	cfg := Config{
		Version: 1,
		Codex: Codex{
			GlobalCommands:  true,
			ModelMap:        defaultModelMap(),
			RootAdapter:     DefaultRootAdapter,
			AgentPreamble:   DefaultAgentPreamble,
			ExcludeDirs:     []string{},
			ExcludeProjects: []string{},
			NeverRegister:   []string{},
			SuffixMode:      "project",
		},
		OpenCode: OpenCode{
			ModelMap:      map[string]string{},
			AgentPreamble: DefaultAgentPreamble,
			Permission:    Permission{},
			MirrorFiles:   []string{},
		},
		Rewrites:    []Rewrite{},
		CommandDirs: []string{},
		Agents:      Agents{PassthroughKeys: []string{}},
		MCP:         MCP{GlobalServers: []string{}, Renamed: map[string]string{}},
		Machine: Machine{
			SourceAgentsDir: "agents",
			ClaudeAgentsDir: "",
			RetiredAgents:   []string{},
			RetiredFiles:    []string{},
			Rewrites:        []Rewrite{},
			CodexHomes:      []string{},
		},
		Sources: map[string]string{},
	}
	for _, row := range Describe(cfg) {
		cfg.Sources[row.Key] = "default"
	}
	return cfg
}

func MachinePath(home string) string { return paths.EngineSyncConfig(home) }

func LoadConfigLayers(root string, homes ...string) (Config, error) {
	home := ""
	if len(homes) > 0 {
		home = homes[0]
	} else {
		var err error
		home, err = paths.Home()
		if err != nil {
			return Config{}, err
		}
	}
	return Resolve(root, home, paths.Getenv)
}

func sourceForError(cfg Config, err error) string {
	key, _, _ := strings.Cut(err.Error(), " ")
	key = strings.TrimSuffix(key, ":")
	// The longest recorded key that is the error's key or one of its parents
	// names the layer; a sibling sharing a prefix ("o" and "opus") never does.
	best, owner := "", ""
	for field, source := range cfg.Sources {
		if (key == field || strings.HasPrefix(key, field+".")) && source != "default" && len(field) > len(best) {
			best, owner = field, source
		}
	}
	if owner != "" {
		return owner
	}
	return cfg.Source
}

func readObject(path string) (map[string]json.RawMessage, bool, error) {
	object, found, err := readObjectWithoutVersion(path)
	if err != nil || !found {
		return object, found, err
	}
	if _, ok := object["version"]; !ok {
		return nil, false, fmt.Errorf("config %s: version is required", path)
	}
	return object, true, nil
}

func readObjectWithoutVersion(path string) (map[string]json.RawMessage, bool, error) {
	raw, err := readConfigFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("config %s: read config: %w", path, err)
	}
	if err := uniqueJSON(raw); err != nil {
		return nil, false, fmt.Errorf("parse config %s: %w", path, err)
	}
	var object map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&object); err != nil {
		return nil, false, fmt.Errorf("config %s: parse config: %w", path, err)
	}
	if object == nil {
		return nil, false, fmt.Errorf("config %s: config must be an object", path)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, false, fmt.Errorf("config %s: config has trailing JSON data", path)
	}
	return object, true, nil
}

func markSources(cfg *Config, object map[string]json.RawMessage, prefix, source string) {
	for key, raw := range object {
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil && nested != nil && len(nested) > 0 {
			markSources(cfg, nested, prefix+key+".", source)
		} else {
			cfg.Sources[prefix+key] = source
		}
	}
}

func validateObject(object map[string]json.RawMessage, shape reflect.Type, prefix string) error {
	fields := map[string]reflect.Type{}
	for i := 0; i < shape.NumField(); i++ {
		f := shape.Field(i)
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if key != "-" {
			fields[key] = f.Type
		}
	}
	for key, raw := range object {
		label := prefix + key
		t, ok := fields[key]
		if !ok {
			return fmt.Errorf("%s: unknown key", label)
		}
		if err := validateValue(raw, t, label); err != nil {
			return err
		}
	}
	return nil
}

func validateValue(raw json.RawMessage, shape reflect.Type, key string) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%s: null is not a %s", key, shape)
	}
	if shape == reflect.TypeFor[Permission]() {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) == nil && object != nil {
			for name, value := range object {
				if err := validateValue(value, reflect.TypeFor[PermissionValue](), key+"."+name); err != nil {
					return err
				}
			}
		}
	}
	if reflect.PointerTo(shape).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		if err := json.Unmarshal(raw, reflect.New(shape).Interface()); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		return nil
	}
	switch shape.Kind() {
	case reflect.Pointer:
		return validateValue(raw, shape.Elem(), key)
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return fmt.Errorf("%s: want object: %w", key, err)
		}
		return validateObject(object, shape, key+".")
	case reflect.Map:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return fmt.Errorf("%s: want object: %w", key, err)
		}
		for name, value := range object {
			if err := validateValue(value, shape.Elem(), key+"."+name); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var list []json.RawMessage
		if err := json.Unmarshal(raw, &list); err != nil {
			return fmt.Errorf("%s: want array: %w", key, err)
		}
		for i, value := range list {
			if err := validateValue(value, shape.Elem(), fmt.Sprintf("%s[%d]", key, i)); err != nil {
				return err
			}
		}
	default:
		if err := json.Unmarshal(raw, reflect.New(shape).Interface()); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	return nil
}

func Validate(cfg Config) error {
	if e := RelativePath(cfg.Machine.SourceAgentsDir, "machine.sourceAgentsDir"); e != nil {
		return e
	}
	if cfg.Machine.AgentMarkerDir != "" {
		if e := RelativePath(cfg.Machine.AgentMarkerDir, "machine.agentMarkerDir"); e != nil {
			return e
		}
	}
	if cfg.Codex.Review.DefaultEffort != "" && !strings.Contains("|low|medium|high|xhigh|", "|"+cfg.Codex.Review.DefaultEffort+"|") {
		return fmt.Errorf("codex.review.defaultEffort: invalid effort %q", cfg.Codex.Review.DefaultEffort)
	}
	for _, home := range cfg.Machine.CodexHomes {
		if !filepath.IsAbs(home) {
			return fmt.Errorf("machine.codexHomes: %q must be absolute", home)
		}
	}
	if cfg.Machine.ClaudeAgentsDir != "" && !filepath.IsAbs(cfg.Machine.ClaudeAgentsDir) {
		if e := RelativePath(cfg.Machine.ClaudeAgentsDir, "machine.claudeAgentsDir"); e != nil {
			return e
		}
	}
	if cfg.Version != 1 {
		return fmt.Errorf("version %d unsupported (want 1)", cfg.Version)
	}
	if cfg.WorkbenchMarker != "" {
		if err := RelativePath(cfg.WorkbenchMarker, "workbenchMarker"); err != nil {
			return err
		}
	}
	for key, values := range map[string][]string{"opencode.mirrorFiles": cfg.OpenCode.MirrorFiles, "machine.retiredFiles": cfg.Machine.RetiredFiles, "codex.excludeDirs": cfg.Codex.ExcludeDirs, "commandDirs": cfg.CommandDirs} {
		for _, value := range values {
			if err := RelativePath(value, key); err != nil {
				return err
			}
		}
	}
	if cfg.Machine.ManagedBy != "" && !managedByValue.MatchString(cfg.Machine.ManagedBy) {
		return &Refusal{Msg: fmt.Sprintf("machine.managedBy: %q must match %s", cfg.Machine.ManagedBy, artifact.ManagedByPattern)}
	}
	if cfg.Targets != nil {
		if err := cfg.Targets.validate(); err != nil {
			return err
		}
	}
	if cfg.Machine.SourceGeneratedAgentsDir != "" {
		if err := RelativePath(cfg.Machine.SourceGeneratedAgentsDir, "machine.sourceGeneratedAgentsDir"); err != nil {
			return err
		}
	}
	for key, values := range map[string][]string{"codex.projects": cfg.Codex.Projects, "codex.excludeProjects": cfg.Codex.ExcludeProjects} {
		for _, value := range values {
			if err := RelativePath(value, key); err != nil {
				return err
			}
		}
	}
	for key, values := range map[string][]string{"codex.neverRegister": cfg.Codex.NeverRegister, "machine.retiredAgents": cfg.Machine.RetiredAgents} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "/\\ \t\n") || value == "." || value == ".." {
				return fmt.Errorf("%s: invalid name %q", key, value)
			}
		}
	}
	for key, models := range map[string]map[string]string{"codex.modelMap": cfg.Codex.ModelMap, "opencode.modelMap": cfg.OpenCode.ModelMap} {
		for alias, model := range models {
			if strings.TrimSpace(alias) == "" || strings.TrimSpace(model) == "" {
				return fmt.Errorf("%s.%s: alias and model must be nonempty", key, alias)
			}
		}
	}
	for _, key := range cfg.Agents.PassthroughKeys {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, " \t\n:") {
			return fmt.Errorf("agents.passthroughKeys: invalid key %q", key)
		}
	}
	for _, section := range []struct {
		key  string
		rows []Rewrite
	}{{"rewrites", cfg.Rewrites}, {"machine.rewrites", cfg.Machine.Rewrites}} {
		for i, row := range section.rows {
			if row.Literal == "" {
				return fmt.Errorf("%s[%d].literal: empty source text", section.key, i)
			}
		}
	}
	if _, err := prepareTemplates(cfg); err != nil {
		return err
	}
	for _, server := range cfg.MCP.GlobalServers {
		if strings.TrimSpace(server) == "" {
			return errors.New("mcp.globalServers: empty server")
		}
	}
	for server, replacement := range cfg.MCP.Renamed {
		if strings.TrimSpace(server) == "" || strings.TrimSpace(replacement) == "" {
			return errors.New("mcp.renamed: server names must be nonempty")
		}
	}
	if cfg.Codex.SuffixMode != "project" && cfg.Codex.SuffixMode != "none" && cfg.Codex.SuffixMode != "strip-prefix" {
		return fmt.Errorf("codex.suffixMode %q must be project, strip-prefix, or none", cfg.Codex.SuffixMode)
	}
	if cfg.Codex.SuffixMode == "strip-prefix" && cfg.Codex.SuffixPrefix == "" {
		return errors.New("codex.suffixPrefix required for strip-prefix")
	}
	return nil
}

// A dangling config symlink is present but unreadable, never a missing policy.
func readConfigFile(path string) ([]byte, error) {
	raw, err := paths.ReadRegular(path)
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Lstat(path); statErr == nil {
			return nil, fmt.Errorf("unreadable config: %v", err)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}
	}
	return raw, err
}

type SourceRow struct {
	Key    string          `json:"key"`
	Value  json.RawMessage `json:"value"`
	Source string          `json:"source"`
}

func Describe(cfg Config) []SourceRow {
	raw, _ := json.Marshal(MachineFile{CommandDirs: cfg.CommandDirs, Version: cfg.Version, Codex: cfg.Codex, OpenCode: cfg.OpenCode, WorkbenchMarker: cfg.WorkbenchMarker, MCP: cfg.MCP, Rewrites: cfg.Rewrites, Machine: cfg.Machine, Targets: cfg.Targets, Agents: cfg.Agents})
	var object map[string]json.RawMessage
	_ = json.Unmarshal(raw, &object)
	var rows []SourceRow
	var visit func(map[string]json.RawMessage, string)
	visit = func(object map[string]json.RawMessage, prefix string) {
		for key, raw := range object {
			var nested map[string]json.RawMessage
			name := prefix + key
			if json.Unmarshal(raw, &nested) == nil && nested != nil && len(nested) > 0 {
				visit(nested, name+".")
			} else {
				source := cfg.Sources[name]
				if source == "" {
					source = "default"
				}
				rows = append(rows, SourceRow{Key: name, Value: raw, Source: source})
			}
		}
	}
	visit(object, "")
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

var RelativePath = paths.RelativePath
