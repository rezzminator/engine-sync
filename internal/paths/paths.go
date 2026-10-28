// Package paths resolves every filesystem location engine-sync reads or
// writes: the home, a workbench marker, the generated agent stores, and the
// engine-sync home, state and config directories.
package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Home is the home directory outputs default to: $HOME.
func Home() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

// HasWorkbenchManifest never follows a symlink in a configured marker path.
func HasWorkbenchManifest(dir string, markers ...string) (bool, error) {
	marker := ""
	if len(markers) > 0 {
		marker = markers[0]
	} else {
		marker = os.Getenv("ENGINE_SYNC_WORKBENCH_MARKER")
	}
	if marker == "" {
		return false, nil
	}
	if err := RelativePath(marker, "workbenchMarker"); err != nil {
		return false, err
	}
	path := filepath.Join(dir, filepath.FromSlash(marker))
	current := dir
	for _, part := range strings.Split(filepath.FromSlash(marker), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect workbench %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("inspect workbench %s: symlink marker refused", current)
		}
		if current == path && !info.Mode().IsRegular() {
			return false, fmt.Errorf("inspect workbench %s: not a regular file", path)
		}
	}
	return true, nil
}

// ClaudeConfigDir is the global source default shared by global and doctor.
func ClaudeConfigDir(env Env, home string) string {
	if value := env("CLAUDE_CONFIG_DIR"); value != "" {
		return value
	}
	return filepath.Join(home, ".claude")
}
func GlobalAgentsSource(home, repo string, configDirs ...string) string {
	if repo == "" {
		return filepath.Join(GlobalSourceRoot(home, repo, configDirs...), "agents")
	}
	return filepath.Join(repo, "agents")
}
func GlobalSourceRoot(home, repo string, configDirs ...string) string {
	if repo == "" {
		if len(configDirs) > 0 && configDirs[0] != "" {
			return configDirs[0]
		}
		return ClaudeConfigDir(os.Getenv, home)
	}
	return repo
}

// PhysicalPath resolves the deepest existing ancestor and keeps a missing tail.
func PhysicalPath(path string) (string, error) {
	candidate := filepath.Clean(path)
	var missing []string
	for {
		physical, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				physical = filepath.Join(physical, missing[i])
			}
			return filepath.Clean(physical), nil
		}
		parent := filepath.Dir(candidate)
		if !errors.Is(err, os.ErrNotExist) || parent == candidate {
			return "", fmt.Errorf("resolve physical path %s: %w", path, err)
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = parent
	}
}

// SameEntry compares parent directories physically without following the leaf link.
func SameEntry(first, second string) (bool, error) {
	a, e := PhysicalPath(filepath.Dir(first))
	if e != nil {
		return false, e
	}
	b, e := PhysicalPath(filepath.Dir(second))
	if e != nil {
		return false, e
	}
	return filepath.Base(first) == filepath.Base(second) && a == b, nil
}

// Env reads one environment variable; os.Getenv in production, a map in a
// test.
type Env func(string) string

// absoluteOr is the variable's value when it is an absolute path, else the
// fallback built from home.
func absoluteOr(env Env, name, home string, rest ...string) string {
	if value := env(name); filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(append([]string{home}, rest...)...)
}

// EngineSyncHome is {engine-sync home}: $ENGINE_SYNC_HOME when absolute, else
// $XDG_DATA_HOME/engine-sync when absolute, else {home}/.local/share/engine-sync.
// The stable binary every caller outside the plugin runs is its bin/engine-sync.
func EngineSyncHome(env Env, home string) string {
	if value := env("ENGINE_SYNC_HOME"); filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(absoluteOr(env, "XDG_DATA_HOME", home, ".local", "share"), "engine-sync")
}

// EngineSyncState is {engine-sync state}: $ENGINE_SYNC_STATE when absolute,
// else $XDG_STATE_HOME/engine-sync when absolute, else
// {home}/.local/state/engine-sync. It holds the dirty flags, the writer
// records and the locks.
func EngineSyncState(env Env, home string) string {
	if value := env("ENGINE_SYNC_STATE"); filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(absoluteOr(env, "XDG_STATE_HOME", home, ".local", "state"), "engine-sync")
}

// EngineSyncConfig is the one global policy file, `machine` and `targets`
// with the rest: {home}/.claude/engine-sync.json. No environment variable
// moves it.
func EngineSyncConfig(home string) string {
	return filepath.Join(home, ".claude", "engine-sync.json")
}
