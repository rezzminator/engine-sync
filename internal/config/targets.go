package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"io/fs"
	"path/filepath"
	"strings"
)

// Targets is {engine-sync config}/targets.json: the machine's Codex homes,
// and optionally the Claude config dirs and the source clone.
type Targets struct {
	CodexHomes       *[]string `json:"codexHomes"`
	ClaudeConfigDirs []string  `json:"claudeConfigDirs,omitempty"`
	SourceRepo       string    `json:"sourceRepo,omitempty"`
	// Capabilities are host capability names an agent's `requires` tests.
	Capabilities []string `json:"capabilities,omitempty"`
}

// Capabilities is the host's capability set: every flag value plus the
// targets.json "capabilities" list, deduplicated, in first-seen order. A
// broken targets file is an error, never an empty set.
func Capabilities(get paths.Env, home string, flags []string) ([]string, error) {
	targets, _, err := LoadTargets(get, home)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, name := range append(append([]string{}, flags...), targets.Capabilities...) {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("capability: empty name")
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// WithCapabilities is a copy of cfg carrying the host's capability set (see
// Capabilities); the cached policy itself is never mutated.
func WithCapabilities(get paths.Env, home string, cfg *Config, flags []string) (*Config, error) {
	capabilities, err := Capabilities(get, home, flags)
	if err != nil {
		return nil, fmt.Errorf("resolve capabilities: %w", err)
	}
	value := *cfg
	value.Capabilities = capabilities
	return &value, nil
}

// LoadTargets returns the targets file, found false when there is none. A
// file that is present but unreadable or unparseable is an error, never an
// empty roster; one without codexHomes leaves CodexHomes nil, the same Codex
// homes as no targets file.
func LoadTargets(get paths.Env, home string) (Targets, bool, error) {
	path := filepath.Join(paths.EngineSyncConfig(get, home), "targets.json")
	raw, err := paths.ReadRegular(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Targets{}, false, nil
	}
	if err != nil {
		return Targets{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var targets Targets
	if err := uniqueJSON(raw); err != nil {
		return Targets{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&targets); err != nil {
		return Targets{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	var homes []string
	if targets.CodexHomes != nil {
		homes = *targets.CodexHomes
	}
	for _, dir := range append(append([]string(nil), homes...), targets.ClaudeConfigDirs...) {
		if !filepath.IsAbs(dir) {
			return Targets{}, false, fmt.Errorf("%s: %q is not an absolute path", path, dir)
		}
	}
	if targets.SourceRepo != "" && !filepath.IsAbs(targets.SourceRepo) {
		return Targets{}, false, fmt.Errorf("%s: sourceRepo must be absolute", path)
	}
	return targets, true, nil
}

// ResolveTargets is the common roster for every machine command. Nil homes
// selects policy; an explicit empty slice disables Codex destinations.
func ResolveTargets(get paths.Env, home string, cfg Config, override Targets) (Targets, error) {
	result := override
	// An explicit roster is also the recovery path for a broken targets file.
	if override.CodexHomes == nil {
		t, found, e := LoadTargets(get, home)
		if e != nil {
			return Targets{}, e
		}
		if found {
			if result.SourceRepo == "" {
				result.SourceRepo = t.SourceRepo
			}
			if len(result.ClaudeConfigDirs) == 0 {
				result.ClaudeConfigDirs = t.ClaudeConfigDirs
			}
		}
		envHome := get("CODEX_HOME")
		switch {
		case envHome != "":
			if !filepath.IsAbs(envHome) {
				return Targets{}, fmt.Errorf("CODEX_HOME %q must be absolute", envHome)
			}
			v := []string{envHome}
			result.CodexHomes = &v
		case found && t.CodexHomes != nil:
			result.CodexHomes = t.CodexHomes
		case cfg.Sources["machine.codexHomes"] != "" && cfg.Sources["machine.codexHomes"] != "default":
			v := append([]string{}, cfg.Machine.CodexHomes...)
			result.CodexHomes = &v
		default:
			v := []string{filepath.Join(home, ".codex")}
			result.CodexHomes = &v
		}
	}
	if len(result.ClaudeConfigDirs) == 0 {
		dir := cfg.ClaudeConfigDir
		if dir == "" {
			dir = filepath.Join(home, ".claude")
		}
		result.ClaudeConfigDirs = []string{dir}
	}
	for _, v := range append(append([]string{}, *result.CodexHomes...), result.ClaudeConfigDirs...) {
		if !filepath.IsAbs(v) {
			return Targets{}, fmt.Errorf("machine destination %q must be absolute", v)
		}
	}
	for i, dir := range result.ClaudeConfigDirs {
		physical, err := paths.PhysicalPath(dir)
		if err != nil {
			return Targets{}, fmt.Errorf("machine destination %s: %w", dir, err)
		}
		result.ClaudeConfigDirs[i] = physical
	}
	return result, nil
}
