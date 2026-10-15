// Package testjail points every test process at a throwaway home before any
// test resolves a path: HOME, TMPDIR and the XDG data, state and config homes
// move into one temp root removed afterwards, and the engine-sync overrides a
// developer's shell may carry are cleared, so no test reads or writes the
// operator's machine.
package testjail

import (
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// clearedEnv are the variables a developer's shell may carry that would steer
// a test toward a real binary, account or project.
var clearedEnv = []string{
	"ENGINE_SYNC_BIN", "ENGINE_SYNC_WORKBENCH_MARKER", "ENGINE_SYNC_HOME", "ENGINE_SYNC_STATE", "CODEX_HOME",
	"PLUGIN_LOG_DIR", "CLAUDE_CONFIG_DIR", "CLAUDE_PROJECT_DIR", "CLAUDE_PLUGIN_ROOT", "CLAUDE_PLUGIN_DATA",
}

// Run jails the process, runs the tests and returns their exit code; a jail
// that cannot be built fails the package (exit 1) before any test runs.
func RunTests(m *testing.M) int {
	base, err := canonicalBase()
	if err != nil {
		warnSetup("no canonical temp base: %v", err)
		return 1
	}
	return runJailedTests(m.Run, base)
}

// canonicalBase is a short temp root with every symlink resolved (macOS's
// /tmp is /private/tmp): a test comparing a path it built with one a tool
// printed sees the same spelling.
func canonicalBase() (string, error) { return filepath.EvalSymlinks(os.TempDir()) }

func runJailedTests(tests func() int, base string) int {
	realHome, err := paths.Home()
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
		"HOME":           home,
		"TMPDIR":         base,
		"XDG_DATA_HOME":  filepath.Join(root, "data"),
		"XDG_STATE_HOME": filepath.Join(root, "state"),
		// Git fixtures read only repository-local configuration.
		"GIT_CONFIG_GLOBAL":   "/dev/null",
		"GIT_CONFIG_NOSYSTEM": "1",
	}
	for _, dir := range []string{home, jailed["XDG_DATA_HOME"], jailed["XDG_STATE_HOME"]} {
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

func Scratch(t testing.TB) string {
	t.Helper()
	base, e := canonicalBase()
	if e != nil {
		t.Fatal(e)
	}
	dir, e := os.MkdirTemp(base, "test-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(dir); e != nil {
			t.Error(e)
		}
	})
	return dir
}
func MkdirTemp(dir, pattern string) (string, error) { return os.MkdirTemp(dir, pattern) }

// unprivilegedEnv carries the parent's pid into the re-run, so a value left in
// a shell never reads as a re-run.
const unprivilegedEnv = "ENGINE_SYNC_TEST_UNPRIVILEGED"

// RanUnprivileged serves a test whose fixture is a permission bit, which root
// ignores. Run as root, it runs the calling test (or subtest), and only it, in
// a copy of this test binary as uid 65534, and reports true once that run
// passed; the caller then returns. Otherwise it reports false and the caller
// runs its body in-process.
func RanUnprivileged(t *testing.T) bool {
	t.Helper()
	if os.Geteuid() != 0 {
		return false
	}
	if os.Getenv(unprivilegedEnv) == strconv.Itoa(os.Getppid()) {
		t.Fatal("the unprivileged re-run is still root")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	// The build directory of the test binary is private to root; the copy sits where uid 65534 can run it.
	dir := Scratch(t)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, filepath.Base(self))
	if err := os.WriteFile(bin, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(t.Name(), "/")
	for i, part := range parts {
		parts[i] = "^" + regexp.QuoteMeta(part) + "$"
	}
	cmd := exec.Command(bin, "-test.run="+strings.Join(parts, "/"), "-test.v", "-test.count=1")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), unprivilegedEnv+"="+strconv.Itoa(os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: "+t.Name()+" ") {
		t.Fatalf("unprivileged run of %s: %v\n%s", t.Name(), err, out)
	}
	return true
}
