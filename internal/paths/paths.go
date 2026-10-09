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

// WorkbenchManifest is the marker of a nested workbench project.
func WorkbenchManifest(dir string) string {
	return filepath.Join(dir, ".professor", "workbench.json")
}

// HasWorkbenchManifest inspects a workbench marker without following links.
func HasWorkbenchManifest(dir string) (bool, error) {
	path := WorkbenchManifest(dir)
	for _, candidate := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(candidate)
		// ENOTDIR: a regular file named .professor holds no manifest.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect workbench %s: %w", candidate, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, nil
		}
		if candidate == path && !info.Mode().IsRegular() {
			return false, fmt.Errorf("inspect workbench %s: not a regular file", path)
		}
	}
	return true, nil
}

// PhysicalPath resolves the deepest existing ancestor and keeps a missing tail.
func PhysicalPath(path string) string {
	candidate := filepath.Clean(path)
	var missing []string
	for {
		physical, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				physical = filepath.Join(physical, missing[i])
			}
			return filepath.Clean(physical)
		}
		parent := filepath.Dir(candidate)
		if !errors.Is(err, os.ErrNotExist) || parent == candidate {
			return filepath.Clean(path)
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = parent
	}
}

// GeneratedCodexAgentsDir is where pfm compiles the machine-global Codex
// agent .toml twins (codexgen.RunGlobalAgents) — pfm-owned state, never
// inside the source clone a template's .md lives in, so a stale compiler on
// any host can never rewrite a tracked file. A `~/.codex/agents/<name>.toml`
// link points here; uninstall removes this whole directory along with the
// links it owns.
func LegacyGeneratedCodexAgentsDir(home string) string {
	return filepath.Join(home, ".local", "state", "pfm", "generated", "codex-agents")
}

// GeneratedClaudeAgentsDir is where pfm renders the machine-global Claude
// agent VARIANTS templates/global/agents/variants.json declares
// (codexgen.RunGlobalAgents) — one source agent re-emitted under another name
// with overridden frontmatter. An original agent is linked straight from the
// clone and never lands here; only a variant's `{config}/agents/<name>.md`
// link points into this directory, and uninstall removes it with those links.
func GeneratedClaudeAgentsDir(home string) string {
	return filepath.Join(home, ".local", "state", "pfm", "generated", "claude-agents")
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

// EngineSyncConfig is {engine-sync config}: $XDG_CONFIG_HOME/engine-sync when
// absolute, else {home}/.config/engine-sync. It holds targets.json.
func EngineSyncConfig(env Env, home string) string {
	return filepath.Join(absoluteOr(env, "XDG_CONFIG_HOME", home, ".config"), "engine-sync")
}
