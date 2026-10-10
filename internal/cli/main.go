// Command engine-sync carries a project's Claude Code configuration to Codex
// and OpenCode: the single writer of every Codex and OpenCode artifact
// derived from Claude sources (docs/interface.md).
package cli

import (
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target"
	"io"
	"os"
	"path/filepath"

	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/guard"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// version is this binary's release; a release build stamps it with
// -ldflags "-X main.version=…", and the plugin manifest names the same one.
var version = "0.1.0"

// env is the process environment one run reads.
type env struct {
	get      paths.Env
	stdin    io.Reader
	notices  map[string]bool
	policies map[string]policyResult
	sources  map[string]*source.Project
}

type policyResult struct {
	config *config.Config
	err    error
}

// Main is the process adapter; all compiler stages are driven by run.
func Main(args []string, stdout, stderr io.Writer, stdin io.Reader, release string) int {
	version = release
	return dispatch(args, stdout, stderr, env{get: paths.Getenv, stdin: stdin})
}

func dispatch(args []string, stdout, stderr io.Writer, e env) int {
	if e.notices == nil {
		e.notices = map[string]bool{}
	}
	if e.policies == nil {
		e.policies = map[string]policyResult{}
	}
	if e.sources == nil {
		e.sources = map[string]*source.Project{}
	}
	if len(args) > 0 && (args[0] == "--version") {
		args[0] = "version"
	}
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		args[0] = "help"
	}
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	for _, entry := range commandTable() {
		if args[0] == entry.Name {
			return entry.Run(args[1:], stdout, stderr, e)
		}
	}

	fmt.Fprintf(stderr, "engine-sync: unknown command %q\n", args[0])
	printUsage(stderr)
	return 2
}

type commandEntry struct {
	Name, Usage string
	Run         func([]string, io.Writer, io.Writer, env) int
}

func commandTable() []commandEntry {
	return []commandEntry{
		{"build", "build [repo] [--engine NAME] [--dry-run] [options]", func(a []string, o, w io.Writer, e env) int { return runProjectTargets("build", a, o, w, e, "") }},
		{"check", "check [repo] [--engine NAME] [options]", func(a []string, o, w io.Writer, e env) int { return runProjectTargets("check", a, o, w, e, "") }},
		{"doctor", "doctor [repo] | doctor --machine [options]", func(a []string, o, w io.Writer, e env) int {
			if containsFlag(a, "--machine") {
				return runMachineDoctor(a, o, w, e)
			}
			return runProjectTargets("doctor", a, o, w, e, "")
		}},
		{"plan", "plan [repo] [--engine NAME] [--json]", func(a []string, o, w io.Writer, e env) int { return runProjectTargets("plan", a, o, w, e, "") }},
		{"codex", "codex build|check|agents [repo] [options]", runCodex},
		{"opencode", "opencode build|check|doctor [repo] [options]", runOpenCode},
		{"config", "config show|validate|schema [--json] [root]", runConfig},
		{"global", "global [options]", runGlobal},
		{"hook", "hook claude mark|stop|start", runHook},
		{"version", "version", func(_ []string, o, w io.Writer, e env) int { fmt.Fprintf(o, "engine-sync %s\n", version); return 0 }},
		{"help", "help", func(_ []string, o, w io.Writer, e env) int { printUsage(o); return 0 }},
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: engine-sync COMMAND [args]")
	for _, c := range commandTable() {
		fmt.Fprintln(w, "  "+c.Usage)
	}
	fmt.Fprint(w, "targets:")
	for _, t := range target.Registry {
		fmt.Fprint(w, " "+t.Name())
	}
	fmt.Fprintln(w)
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

func (e env) source(root, home string, cfg config.Config) *source.Project {
	key := root + "\x00" + home
	if p := e.sources[key]; p != nil {
		return p
	}
	p := source.LoadProject(root, home, cfg)
	if e.sources != nil {
		e.sources[key] = &p
	}
	return &p
}

func containsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
