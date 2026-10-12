package cli

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/cliargs"
	"github.com/rezzminator/engine-sync/internal/machine"
	"io"
	"os"

	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/target/codex"
)

// runCodex is the command adapter around the Codex compiler: the compiler
// owns discovery and reconciliation; this layer owns argv, exit status and
// the operator-visible report.
func runCodex(args []string, stdout, stderr io.Writer, e env) int {
	if len(args) == 0 {
		printCodexUsage(stderr)
		return 2
	}
	switch args[0] {
	case "agents":
		return runCodexAgents(args[1:], stdout, stderr, e)
	case "help", "-h", "--help":
		printCodexUsage(stdout)
		return 0
	}
	if args[0] != "build" && args[0] != "check" {
		fmt.Fprintf(stderr, "engine-sync codex: unknown action %q\n", args[0])
		printCodexUsage(stderr)
		return 2
	}
	return runProjectTargets(args[0], args[1:], stdout, stderr, e, "codex")

}

// runCodexAgents compiles the global (host-level) agents: no repository root,
// every source and destination anchored on --home.
func runCodexAgents(args []string, stdout, stderr io.Writer, e env) int {
	flags := NewFlagSet("codex agents", "usage: engine-sync codex agents [--home PATH] [--source-repo PATH] [--capability NAME]...", stderr)
	home := flags.String("home", "", "host HOME whose global agents get compiled and installed")
	sourceRepo := flags.String("source-repo", "", "repository whose configured agent directory supplies the roles")
	var capabilities repeatString
	flags.Var(&capabilities, "capability", "a host capability an agent's requires may name; repeatable")
	positionals, code, ok := ParseFlagsAnywhere(flags, args)
	if !ok {
		return code
	}
	if len(positionals) != 0 {
		flags.Usage()
		return 2
	}
	resolvedHome, err := absoluteHome(*home, e)
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync codex agents: resolve home: %v\n", err)
		return 1
	}
	policy, err := e.policy("", resolvedHome, stderr)
	if err == nil {
		policy, err = config.WithCapabilities(e.get, resolvedHome, policy, capabilities)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	options, err := globalOptions(e, resolvedHome, *sourceRepo, "", nil, nil, false)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	code, err = guarded(e, machine.GuardRoots(options.Home, options.CodexHomes, options.ClaudeConfigDirs), true, func() int {
		result, err := machine.RunGlobalAgents(machine.GlobalAgentsOptions{Home: resolvedHome, SourceRepo: options.SourceRepo, Config: policy, ClaudeConfigDirs: options.ClaudeConfigDirs, CodexHomes: options.CodexHomes})
		if err != nil {
			fmt.Fprintf(stderr, "engine-sync codex agents: %v\n", err)
			return 1
		}
		return machine.PrintGlobalAgents(result, stdout, stderr)
	})
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync codex agents: %v\n", err)
		return 1
	}
	return code
}

// repoRoot is the positional root made absolute, else the nearest ancestor
// of the working directory holding a workbench marker or .git, else the
// first holding a regular CLAUDE.md.
func repoRoot(positionals []string, environments ...env) (string, error) {
	e := env{get: paths.Getenv}
	if len(environments) > 0 {
		e = environments[0]
	}
	home, err := absoluteHome("", e)
	if err != nil {
		return "", err
	}
	if len(positionals) == 1 {
		return paths.ProjectRoot(positionals[0], home)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	policy, err := e.policy("", home, io.Discard)
	if err != nil {
		return "", err
	}
	return paths.DiscoverRoot(cwd, home, policy.WorkbenchMarker)
}

// runOpenCode is the OpenCode compiler's command, behind the same guard.
func runOpenCode(args []string, stdout, stderr io.Writer, e env) int {
	if len(args) == 0 {
		printOpenCodeUsage(stderr)
		return 2
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printOpenCodeUsage(stdout)
		return 0
	}
	if _, ok := parseOpenCodeMode(args[0]); !ok {
		fmt.Fprintf(stderr, "engine-sync opencode: unknown action %q\n", args[0])
		return 2
	}
	return runProjectTargets(args[0], args[1:], stdout, stderr, e, "opencode")
}

func (e env) policy(root, home string, stderr io.Writer) (*config.Config, error) {
	return config.ResolveCached(root, home, e.get, e.policies, e.notices, stderr)
}

type repeatString = cliargs.RepeatString

var reportRunFailure = cliargs.ReportFailure
var printCodexUsage = cliargs.CodexUsage

func parseCodexOptions(action string, args []string, stdout, stderr io.Writer, e env, selections ...*selection) (codex.Options, bool, int, bool) {
	return cliargs.ParseProjectOptions(action, args, stdout, stderr, func(p []string) (string, error) { return repoRoot(p, e) }, e.home, selections...)
}
