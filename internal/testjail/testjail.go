// Package testjail points every test process at a throwaway home before any
// test resolves a path: HOME, TMPDIR and the XDG data, state and config homes
// move into one temp root removed afterwards, and the engine-sync overrides a
// developer's shell may carry are cleared, so no test reads or writes the
// operator's machine.
package testjail

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearedEnv are the variables a developer's shell may carry that would steer
// a test toward a real binary, account or project.
var clearedEnv = []string{
	"ENGINE_SYNC_BIN", "ENGINE_SYNC_HOME", "ENGINE_SYNC_STATE", "CODEX_HOME",
	"CLAUDE_CONFIG_DIR", "CLAUDE_PROJECT_DIR", "CLAUDE_PLUGIN_ROOT", "CLAUDE_PLUGIN_DATA",
}

// Run jails the process, runs the tests and returns their exit code; a jail
// that cannot be built fails the package (exit 1) before any test runs.
func Run(m *testing.M) int {
	base, err := canonicalBase()
	if err != nil {
		warnSetup("no canonical temp base: %v", err)
		return 1
	}
	return run(m.Run, base)
}

// canonicalBase is a short temp root with every symlink resolved (macOS's
// /tmp is /private/tmp): a test comparing a path it built with one a tool
// printed sees the same spelling.
func canonicalBase() (string, error) {
	base, err := filepath.EvalSymlinks("/tmp")
	if err == nil {
		return base, nil
	}
	fallback, fallbackErr := filepath.EvalSymlinks(os.TempDir())
	if fallbackErr != nil {
		return "", errors.Join(err, fallbackErr)
	}
	return fallback, nil
}

func run(tests func() int, base string) int {
	realHome, err := os.UserHomeDir()
	if err != nil {
		warnSetup("real home unknown, the jail cannot compare against it: %v", err)
		realHome = ""
	}
	root, err := os.MkdirTemp(base, "engine-sync-jail-")
	if err != nil {
		warnSetup("no jail root under %s: %v", base, err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil && !errors.Is(err, fs.ErrNotExist) {
			warnSetup("remove jail root %s: %v", root, err)
		}
	}()
	home := filepath.Join(root, "home")
	jailed := map[string]string{
		"HOME":            home,
		"TMPDIR":          base,
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		// Git fixtures read only repository-local configuration.
		"GIT_CONFIG_GLOBAL":   "/dev/null",
		"GIT_CONFIG_NOSYSTEM": "1",
	}
	for _, dir := range []string{home, jailed["XDG_DATA_HOME"], jailed["XDG_STATE_HOME"], jailed["XDG_CONFIG_HOME"]} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			warnSetup("create jail directory %s: %v", dir, err)
			return 1
		}
	}
	if err := refuseRealHome(home, realHome); err != nil {
		warnSetup("%v", err)
		return 1
	}
	for _, name := range clearedEnv {
		if err := os.Unsetenv(name); err != nil {
			warnSetup("clear %s: %v", name, err)
			return 1
		}
	}
	for name, value := range jailed {
		if err := os.Setenv(name, value); err != nil {
			warnSetup("set %s to %q: %v", name, value, err)
			return 1
		}
	}
	return tests()
}

// refuseRealHome fails the jail when its home is, or sits inside, the
// operator's real one.
func refuseRealHome(home, realHome string) error {
	if realHome == "" {
		return nil
	}
	clean := filepath.Clean(home)
	real := filepath.Clean(realHome)
	if clean == real || strings.HasPrefix(clean, real+string(filepath.Separator)) {
		return fmt.Errorf("jail home %s is inside the real home %s", clean, real)
	}
	return nil
}

func warnSetup(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "testjail: "+format+"\n", args...)
}
