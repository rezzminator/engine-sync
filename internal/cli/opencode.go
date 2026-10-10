package cli

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target/opencode"
	"io"
	"path/filepath"
	"strings"
)

// OpenCodeCommand is one parsed `engine-sync opencode build|check|doctor`.
type OpenCodeCommand struct {
	Action string
	Root   string
	Home   string
	Mode   opencode.Mode
	JSON   bool
	Config *config.Config
	Source *source.Project
}

// RunOpenCodeCommand is the operator-facing adapter for the compiler. The
// cmd/engine-sync layer supplies the repository-root resolver and default
// home, so the compiler package remains independently testable.
func RunOpenCodeCommand(args []string, resolveRoot func() (string, error), defaultHome string, stdout, stderr io.Writer) int {
	command, code, ok := ParseOpenCodeCommand(args, resolveRoot, defaultHome, stdout, stderr)
	if !ok {
		return code
	}
	return command.Execute(stdout, stderr)
}

// ParseOpenCodeCommand reads argv; a false ok returns the exit code to stop with
// (0 for help, 1 for an unresolvable root or home, 2 for a usage error).
func ParseOpenCodeCommand(
	args []string, resolveRoot func() (string, error), defaultHome string, stdout, stderr io.Writer,
) (OpenCodeCommand, int, bool) {
	if len(args) == 0 {
		printOpenCodeUsage(stderr)
		return OpenCodeCommand{}, 2, false
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printOpenCodeUsage(stdout)
		return OpenCodeCommand{}, 0, false
	}
	mode, ok := parseOpenCodeMode(args[0])
	if !ok {
		fmt.Fprintf(stderr, "engine-sync opencode: unknown action %q\n", args[0])
		printOpenCodeUsage(stderr)
		return OpenCodeCommand{}, 2, false
	}
	flags := NewFlagSet("opencode "+args[0], "usage: engine-sync opencode build|check|doctor [repo-root] [options]", stderr)
	home := flags.String("home", "", "OpenCode global source/output home")
	asJSON := flags.Bool("json", false, "print one JSON result object instead of the report")
	positionals, code, parsed := ParseFlagsAnywhere(flags, args[1:])
	if !parsed {
		return OpenCodeCommand{}, code, false
	}
	if len(positionals) > 1 {
		flags.Usage()
		return OpenCodeCommand{}, 2, false
	}
	var root string
	var err error
	if len(positionals) == 1 {
		root, err = filepath.Abs(positionals[0])
	} else {
		root, err = resolveRoot()
	}
	// A resolve failure under --json is the one object's error.
	failed := func(what string, err error) (OpenCodeCommand, int, bool) {
		if *asJSON {
			return OpenCodeCommand{}, diag.WriteJSON(stdout, stderr, diag.Report{}, fmt.Errorf("%s: %w", what, err)), false
		}
		fmt.Fprintf(stderr, "engine-sync opencode %s: %s: %v\n", args[0], what, err)
		return OpenCodeCommand{}, 1, false
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
	return OpenCodeCommand{Action: args[0], Root: root, Home: homePath, Mode: mode, JSON: *asJSON}, 0, true
}

// Execute compiles, reports and returns the exit code.
func (command OpenCodeCommand) Execute(stdout, stderr io.Writer) int {
	result, err := opencode.Compile(opencode.Options{Root: command.Root, Home: command.Home, Mode: command.Mode, Config: command.Config, Source: command.Source})
	if command.JSON {
		return diag.WriteJSON(stdout, stderr, result, err)
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
	if command.Mode == opencode.ModeBuild {
		for _, action := range result.Actions {
			if action.Kind == "delete" {
				fmt.Fprintf(stdout, "engine-sync opencode: deleted %s\n", action.Path)
			}
		}
	}
	if !result.OK {
		return 1
	}
	if command.Mode == opencode.ModeBuild {
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

func parseOpenCodeMode(action string) (opencode.Mode, bool) {
	switch action {
	case "build":
		return opencode.ModeBuild, true
	case "check":
		return opencode.ModeCheck, true
	case "doctor":
		return opencode.ModeDoctor, true
	default:
		return 0, false
	}
}

func printOpenCodeUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: engine-sync opencode build|check|doctor [repo-root] [options]")
	fmt.Fprintln(w, "  --home PATH    OpenCode global source/output home")
	fmt.Fprintln(w, "  --json         one JSON result object on stdout")
}
