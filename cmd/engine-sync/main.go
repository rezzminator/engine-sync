// Command engine-sync carries a project's Claude Code configuration to Codex
// and OpenCode: the single writer of every Codex and OpenCode artifact
// derived from Claude sources (docs/interface.md).
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rezzminator/engine-sync/internal/guard"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// version is this binary's release; a release build stamps it with
// -ldflags "-X main.version=…", and the plugin manifest names the same one.
var version = "0.1.0"

// env is the process environment one run reads.
type env struct {
	get   paths.Env
	stdin io.Reader
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, env{get: os.Getenv, stdin: os.Stdin}))
}

func run(args []string, stdout, stderr io.Writer, e env) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(stdout, "engine-sync %s\n", version)
		return 0
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "codex":
		return runCodex(args[1:], stdout, stderr, e)
	case "opencode":
		return runOpenCode(args[1:], stdout, stderr, e)
	case "build", "check":
		return runBoth(args[0], args[1:], stdout, stderr, e)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr, e)
	case "global":
		return runGlobal(args[1:], stdout, stderr, e)
	case "hook":
		return runHook(args[1:], stdout, stderr, e)
	}
	fmt.Fprintf(stderr, "engine-sync: unknown command %q\n", args[0])
	printUsage(stderr)
	return 2
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: engine-sync COMMAND [args]")
	fmt.Fprintln(w, "  build|check [repo] [options]          both mirrors (codex, then opencode)")
	fmt.Fprintln(w, "  doctor [repo] | doctor --machine      codex check + opencode doctor | machine rows")
	fmt.Fprintln(w, "  codex build|check|agents [repo]       the Codex mirror (pfm codex's successor)")
	fmt.Fprintln(w, "  opencode build|check|doctor [repo]    the OpenCode mirror (pfm opencode's successor)")
	fmt.Fprintln(w, "  global [options]                      Codex roles, global agents and commands for each home")
	fmt.Fprintln(w, "  hook claude mark|stop|start           the Claude plugin's hooks")
	fmt.Fprintln(w, "  version")
}

// home is the run's home directory: $HOME.
func (e env) home() (string, error) {
	if home := e.get("HOME"); home != "" {
		return home, nil
	}
	return paths.Home()
}

func (e env) state(home string) string { return paths.EngineSyncState(e.get, home) }

// writerPath names this binary in a writer record.
func writerPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "engine-sync (path unknown: " + err.Error() + ")"
	}
	return exe
}

// guarded runs write under the lock of every output root it writes, taken in
// the order given, once the version guard, consulted while the locks are held,
// admits it; then it records this binary as each root's writer: a newer writer
// that held a lock while this one waited refuses it. A check (write == false)
// consults the guard but takes no lock and records nothing. Every caller
// names a repository root before the home's "global:" root, so two writers
// never wait on each other's locks.
func guarded(e env, roots []string, writes bool, body func() int) (int, error) {
	home, err := e.home()
	if err != nil {
		return 1, err
	}
	state := e.state(home)
	keys := make([]string, len(roots))
	for i, root := range roots {
		keys[i] = filepath.Clean(root)
	}
	if !writes {
		for _, key := range keys {
			if err := guard.Check(state, key, version); err != nil {
				return 1, err
			}
		}
		return body(), nil
	}
	var releases []func() error
	releaseAll := func() error {
		var errs []error
		for i := len(releases) - 1; i >= 0; i-- {
			errs = append(errs, releases[i]())
		}
		return errors.Join(errs...)
	}
	for _, key := range keys {
		release, err := guard.Lock(state, key)
		if err != nil {
			return 1, errors.Join(err, releaseAll())
		}
		releases = append(releases, release)
	}
	for _, key := range keys {
		if err := guard.Check(state, key, version); err != nil {
			return 1, errors.Join(err, releaseAll())
		}
	}
	code := body()
	var saveErr error
	if code == 0 {
		for _, key := range keys {
			saveErr = errors.Join(saveErr, guard.Save(state, key, version, writerPath()))
		}
	}
	if err := releaseAll(); err != nil {
		saveErr = errors.Join(saveErr, err)
	}
	if saveErr != nil {
		return 1, saveErr
	}
	return code, nil
}

// globalRoot is the guard root of a home's machine-level outputs: its Codex
// roles, Claude agent links and global commands.
func globalRoot(home string) string { return "global:" + home }
