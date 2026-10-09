package opencodegen

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/rezzminator/engine-sync/internal/cli"
)

// Command is one parsed `engine-sync opencode build|check|doctor`.
type Command struct {
	Action string
	Root   string
	Home   string
	Mode   Mode
	JSON   bool
}

// RunCommand is the operator-facing adapter for the compiler. The
// cmd/engine-sync layer supplies the repository-root resolver and default
// home, so the compiler package remains independently testable.
func RunCommand(args []string, resolveRoot func() (string, error), defaultHome string, stdout, stderr io.Writer) int {
	command, code, ok := ParseCommand(args, resolveRoot, defaultHome, stdout, stderr)
	if !ok {
		return code
	}
	return command.Execute(stdout, stderr)
}

// ParseCommand reads argv; a false ok returns the exit code to stop with
// (0 for help, 1 for an unresolvable root or home, 2 for a usage error).
func ParseCommand(
	args []string, resolveRoot func() (string, error), defaultHome string, stdout, stderr io.Writer,
) (Command, int, bool) {
	if len(args) == 0 {
		printOpenCodeUsage(stderr)
		return Command{}, 2, false
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printOpenCodeUsage(stdout)
		return Command{}, 0, false
	}
	mode, ok := parseMode(args[0])
	if !ok {
		fmt.Fprintf(stderr, "engine-sync opencode: unknown action %q\n", args[0])
		printOpenCodeUsage(stderr)
		return Command{}, 2, false
	}
	flags := cli.NewFlagSet("opencode "+args[0], "usage: engine-sync opencode build|check|doctor [repo-root] [options]", stderr)
	home := flags.String("home", "", "OpenCode global source/output home")
	asJSON := flags.Bool("json", false, "print one JSON result object instead of the report")
	positionals, code, parsed := cli.ParseFlagsAnywhere(flags, args[1:])
	if !parsed {
		return Command{}, code, false
	}
	if len(positionals) > 1 {
		flags.Usage()
		return Command{}, 2, false
	}
	var root string
	var err error
	if len(positionals) == 1 {
		root, err = filepath.Abs(positionals[0])
	} else {
		root, err = resolveRoot()
	}
	// A resolve failure under --json is the one object's error.
	failed := func(what string, err error) (Command, int, bool) {
		if *asJSON {
			return Command{}, WriteJSON(stdout, stderr, Result{}, fmt.Errorf("%s: %w", what, err)), false
		}
		fmt.Fprintf(stderr, "engine-sync opencode %s: %s: %v\n", args[0], what, err)
		return Command{}, 1, false
	}
	if err != nil {
		return failed("resolve repo root", err)
	}
	homePath := defaultHome
	if *home != "" {
		homePath = *home
	}
	if homePath, err = filepath.Abs(homePath); err != nil {
		return failed("resolve home", err)
	}
	return Command{Action: args[0], Root: root, Home: homePath, Mode: mode, JSON: *asJSON}, 0, true
}

// Execute compiles, reports and returns the exit code.
func (command Command) Execute(stdout, stderr io.Writer) int {
	result, err := Compile(Options{Root: command.Root, Home: command.Home, Mode: command.Mode})
	if command.JSON {
		return WriteJSON(stdout, stderr, result, err)
	}
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync opencode %s: %v\n", command.Action, err)
		return 1
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(stderr, "engine-sync opencode: warning: %s\n", warning)
	}
	for _, problem := range result.Problems {
		fmt.Fprintf(stderr, "engine-sync opencode: %s\n", problem)
	}
	if command.Mode == ModeBuild {
		for _, action := range result.Actions {
			if action.Kind == actionDelete {
				fmt.Fprintf(stdout, "engine-sync opencode: deleted %s\n", action.Path)
			}
		}
	}
	if !result.OK {
		return 1
	}
	if command.Mode == ModeBuild {
		fmt.Fprintf(
			stdout,
			"OPENCODE BUILD PASS wrote=%d unchanged=%d deleted=%d\n",
			result.Wrote,
			result.Unchanged,
			result.Deleted,
		)
		return 0
	}
	fmt.Fprintf(stdout, "OPENCODE %s PASS\n", strings.ToUpper(command.Action))
	return 0
}

func parseMode(action string) (Mode, bool) {
	switch action {
	case "build":
		return ModeBuild, true
	case "check":
		return ModeCheck, true
	case "doctor":
		return ModeDoctor, true
	default:
		return 0, false
	}
}

func printOpenCodeUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: engine-sync opencode build|check|doctor [repo-root] [options]")
	fmt.Fprintln(w, "  --home PATH    OpenCode global source/output home")
	fmt.Fprintln(w, "  --json         one JSON result object on stdout")
}

// JSONResult is the --json shape every engine-sync compile command prints
// (docs/interface.md): rebuildable is the multiset of problems a build would
// repair, error a run failure.
type JSONResult struct {
	OK          bool     `json:"ok"`
	Problems    []string `json:"problems"`
	Rebuildable []string `json:"rebuildable"`
	Warnings    []string `json:"warnings"`
	Wrote       int      `json:"wrote"`
	Unchanged   int      `json:"unchanged"`
	Deleted     int      `json:"deleted"`
	Error       string   `json:"error"`
}

// WriteJSON prints one result as JSON and returns its exit code: 0 when it
// passed, 1 on a finding or a run failure.
func WriteJSON(stdout, stderr io.Writer, result Result, runErr error) int {
	out := JSONResult{
		OK: result.OK && runErr == nil, Problems: nonNil(result.Problems), Rebuildable: nonNil(result.Rebuildable),
		Warnings: nonNil(result.Warnings), Wrote: result.Wrote, Unchanged: result.Unchanged, Deleted: result.Deleted,
	}
	if runErr != nil {
		out.Error = runErr.Error()
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		fmt.Fprintf(stderr, "engine-sync: write JSON result: %v\n", err)
		return 1
	}
	if !out.OK {
		return 1
	}
	return 0
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
