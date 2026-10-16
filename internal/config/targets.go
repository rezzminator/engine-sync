package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
)

// Targets is the global file's `targets` object: the machine's Codex homes,
// and optionally the Claude config dirs, the source clone and the host
// capabilities.
type Targets struct {
	CodexHomes       *[]string `json:"codexHomes,omitempty"`
	ClaudeConfigDirs []string  `json:"claudeConfigDirs,omitempty"`
	SourceRepo       string    `json:"sourceRepo,omitempty"`
	// Capabilities are host capability names an agent's `requires` tests.
	Capabilities []string `json:"capabilities,omitempty"`
}

func (t Targets) validate() error {
	var homes []string
	if t.CodexHomes != nil {
		homes = *t.CodexHomes
	}
	for key, dirs := range map[string][]string{"targets.codexHomes": homes, "targets.claudeConfigDirs": t.ClaudeConfigDirs} {
		for _, dir := range dirs {
			if !filepath.IsAbs(dir) {
				return fmt.Errorf("%s: %q is not an absolute path", key, dir)
			}
		}
	}
	if t.SourceRepo != "" && !filepath.IsAbs(t.SourceRepo) {
		return fmt.Errorf("targets.sourceRepo: %q must be absolute", t.SourceRepo)
	}
	return nil
}

// Capabilities is the host's capability set: every flag value plus the
// global file's targets.capabilities list, deduplicated, in first-seen order. A
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

// LoadTargets returns the global file's `targets`, found false when the file
// has none. A file that is present but unreadable or unparseable is an error,
// never an empty roster; a `targets` without codexHomes leaves CodexHomes
// nil, the same Codex homes as no targets at all. With no global file, an old
// config path is refused (see refuseOldPaths).
func LoadTargets(get paths.Env, home string) (Targets, bool, error) {
	path := MachinePath(home)
	object, found, err := readObject(path)
	if err != nil {
		return Targets{}, false, err
	}
	if !found {
		return Targets{}, false, refuseOldPaths(home, get)
	}
	raw, ok := object["targets"]
	if !ok {
		return Targets{}, false, nil
	}
	if err := validateValue(raw, reflect.TypeFor[*Targets](), "targets"); err != nil {
		return Targets{}, false, fmt.Errorf("config %s: %w", path, err)
	}
	var targets Targets
	if err := json.Unmarshal(raw, &targets); err != nil {
		return Targets{}, false, fmt.Errorf("config %s: targets: %w", path, err)
	}
	if err := targets.validate(); err != nil {
		return Targets{}, false, fmt.Errorf("config %s: %w", path, err)
	}
	return targets, true, nil
}

// oldGlobalNames are the files a former release read the machine policy and
// the targets from, in its config directory.
var oldGlobalNames = []string{"config.json", "targets.json"}

// oldGlobalDirs are the config directories a former release read:
// {home}/.config/engine-sync, and $XDG_CONFIG_HOME/engine-sync when that is
// absolute.
func oldGlobalDirs(home string, env paths.Env) []string {
	dirs := []string{filepath.Join(home, ".config", "engine-sync")}
	if configHome := env("XDG_CONFIG_HOME"); filepath.IsAbs(configHome) {
		if dir := filepath.Join(configHome, "engine-sync"); dir != dirs[0] {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// refuseOldPaths is the loud end of the move to {home}/.claude/engine-sync.json:
// with no global file, an old path that exists is refused naming both paths.
// A missing path, or a non-directory where its parent should be, holds none.
func refuseOldPaths(home string, env paths.Env) error {
	for _, dir := range oldGlobalDirs(home, env) {
		for _, name := range oldGlobalNames {
			old := filepath.Join(dir, name)
			if _, err := os.Lstat(old); errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				continue
			} else if err != nil {
				return fmt.Errorf("inspect %s: %w", old, err)
			}
			where := "into " + MachinePath(home)
			if name == "targets.json" {
				where = `under "targets" in ` + MachinePath(home)
			}
			return &Refusal{Msg: fmt.Sprintf("engine-sync: %s is no longer read; move its keys %s", old, where)}
		}
	}
	return nil
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
