package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// Resolve layers defaults < machine < project < environment. Flags use WithCodexOverrides.
func Resolve(root, home string, env func(string) string) (Config, error) {
	cfg := Default()
	cfg.ClaudeConfigDir = env("CLAUDE_CONFIG_DIR")
	if cfg.ClaudeConfigDir != "" && !filepath.IsAbs(cfg.ClaudeConfigDir) {
		return Config{}, fmt.Errorf("config env CLAUDE_CONFIG_DIR: must be absolute, got %q", cfg.ClaudeConfigDir)
	}
	if cfg.ClaudeConfigDir != "" {
		cfg.Sources["machine.claudeAgentsDir"] = "env CLAUDE_CONFIG_DIR"
	}
	if cfg.ClaudeConfigDir == "" {
		cfg.ClaudeConfigDir = filepath.Join(home, ".claude")
	}
	cfg.Machine.ClaudeAgentsDir = filepath.Join(cfg.ClaudeConfigDir, "agents")
	machinePath := MachinePath(home, env)
	if object, found, err := readObject(machinePath); err != nil {
		return Config{}, err
	} else if found {
		if err := validateObject(object, reflect.TypeFor[MachineFile](), ""); err != nil {
			return Config{}, fmt.Errorf("config %s: %w", machinePath, err)
		}
		agents, err := mergeAgents(object["opencode"], cfg.OpenCode.Agent)
		if err != nil {
			return Config{}, fmt.Errorf("config %s: opencode.agent: %w", machinePath, err)
		}
		machine := MachineFile{CommandDirs: cfg.CommandDirs, Version: cfg.Version, Codex: cfg.Codex, OpenCode: cfg.OpenCode, WorkbenchMarker: cfg.WorkbenchMarker, MCP: cfg.MCP, Rewrites: cfg.Rewrites, Machine: cfg.Machine}
		raw, _ := json.Marshal(object)
		if err := json.Unmarshal(raw, &machine); err != nil {
			return Config{}, fmt.Errorf("config %s: %w", machinePath, err)
		}
		machine.OpenCode.Agent = agents
		cfg.Version, cfg.Codex, cfg.OpenCode, cfg.WorkbenchMarker, cfg.MCP, cfg.Machine, cfg.Rewrites = machine.Version, machine.Codex, machine.OpenCode, machine.WorkbenchMarker, machine.MCP, machine.Machine, machine.Rewrites
		cfg.CommandDirs = machine.CommandDirs
		markSources(&cfg, object, "", machinePath)
	}
	// Cross-key rules run once, on the merged layers: a project may complete
	// what the machine file starts.
	if root == "" {
		if marker := env("ENGINE_SYNC_WORKBENCH_MARKER"); marker != "" {
			cfg.WorkbenchMarker = marker
			cfg.Sources["workbenchMarker"] = "env ENGINE_SYNC_WORKBENCH_MARKER"
		}
		if err := Validate(cfg); err != nil {
			source := sourceForError(cfg, err)
			if source == "" {
				source = machinePath
			}
			return Config{}, fmt.Errorf("config %s: %w", source, err)
		}
		return prepareTemplates(cfg)
	}
	path := filepath.Join(root, filepath.FromSlash(RelPath))
	cfg.Source = path
	object, found, err := readObject(path)
	if err != nil {
		return Config{}, err
	}
	if found {
		for _, rel := range []string{LegacyRelPath, ".claude/opencode-build.json"} {
			legacyPath := filepath.Join(root, rel)
			if _, e := os.Lstat(legacyPath); e == nil {
				cfg.Deprecations = append(cfg.Deprecations, legacyPath+" is ignored because "+path+" exists")
			} else if !os.IsNotExist(e) {
				return Config{}, fmt.Errorf("inspect legacy config %s: %w", legacyPath, e)
			}
		}
		if err := validateObject(object, reflect.TypeFor[Config](), ""); err != nil {
			return Config{}, fmt.Errorf("config %s: %w", path, err)
		}
		agents, err := mergeAgents(object["opencode"], cfg.OpenCode.Agent)
		if err != nil {
			return Config{}, fmt.Errorf("config %s: opencode.agent: %w", path, err)
		}
		raw, _ := json.Marshal(object)
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("config %s: %w", path, err)
		}
		cfg.OpenCode.Agent = agents
		markSources(&cfg, object, "", path)
	} else {
		path = filepath.Join(root, filepath.FromSlash(LegacyRelPath))
		legacy, found, err := readObject(path)
		if err != nil {
			return Config{}, err
		}
		if found {
			for key, raw := range legacy {
				if key != "version" && string(bytes.TrimSpace(raw)) == "null" {
					delete(legacy, key)
				}
			}
			version := legacy["version"]
			cfg.Sources["version"] = path
			delete(legacy, "version")
			if err := validateValue(version, reflect.TypeFor[int](), "version"); err != nil {
				return Config{}, fmt.Errorf("config %s: %w", path, err)
			}
			if err := json.Unmarshal(version, &cfg.Version); err != nil {
				return Config{}, fmt.Errorf("config %s: version: %w", path, err)
			}
			if err := validateObject(legacy, reflect.TypeFor[Codex](), ""); err != nil {
				return Config{}, fmt.Errorf("config %s: %w", path, err)
			}
			raw, _ := json.Marshal(legacy)
			if err := json.Unmarshal(raw, &cfg.Codex); err != nil {
				return Config{}, fmt.Errorf("config %s: %w", path, err)
			}
			// A legacy file's text was emitted verbatim: its braces stay
			// literal once the templates render it (escaped here, once).
			for key, text := range map[string]*string{"rootAdapter": &cfg.Codex.RootAdapter, "agentPreamble": &cfg.Codex.AgentPreamble} {
				if _, set := legacy[key]; set {
					*text = strings.ReplaceAll(*text, "{{", `{{"{{"}}`)
				}
			}
			cfg.Source, cfg.Legacy = path, true
			markSources(&cfg, legacy, "codex.", path)
		}
		path = filepath.Join(root, ".claude", "opencode-build.json")
		if legacy, found, err := readObjectWithoutVersion(path); err != nil {
			return Config{}, err
		} else if found {
			var shape struct {
				ModelMap map[string]string `json:"modelMap"`
			}
			if err := validateObject(legacy, reflect.TypeOf(shape), ""); err != nil {
				return Config{}, fmt.Errorf("config %s: %w", path, err)
			}
			shape.ModelMap = cfg.OpenCode.ModelMap
			raw, _ := json.Marshal(legacy)
			if err := json.Unmarshal(raw, &shape); err != nil {
				return Config{}, fmt.Errorf("config %s: modelMap: %w", path, err)
			}
			cfg.OpenCode.ModelMap = shape.ModelMap
			cfg.Deprecations = append(cfg.Deprecations, path+" is deprecated; use "+filepath.Join(root, filepath.FromSlash(RelPath))+" opencode.modelMap")
			markSources(&cfg, legacy, "opencode.", path)
		}
	}
	if marker := env("ENGINE_SYNC_WORKBENCH_MARKER"); marker != "" {
		cfg.WorkbenchMarker = marker
		cfg.Sources["workbenchMarker"] = "env ENGINE_SYNC_WORKBENCH_MARKER"
	}
	if err := Validate(cfg); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", sourceForError(cfg, err), err)
	}
	return prepareTemplates(cfg)
}

// uniqueJSON rejects duplicates before decoding can discard their first value.
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var value func(string) error
	value = func(path string) error {
		token, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		if delim == '{' {
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				name := key.(string)
				if seen[name] {
					return fmt.Errorf("%s%s: duplicate key", path, name)
				}
				seen[name] = true
				if e := value(path + name + "."); e != nil {
					return e
				}
			}
		} else if delim == '[' {
			for d.More() {
				if e := value(path); e != nil {
					return e
				}
			}
		} else {
			return fmt.Errorf("unexpected delimiter %q", delim)
		}
		_, e = d.Token()
		return e
	}
	if e := value(""); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
