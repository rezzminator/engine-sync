package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rezzminator/engine-sync/internal/cli"
	"github.com/rezzminator/engine-sync/internal/codexgen"
	"github.com/rezzminator/engine-sync/internal/opencodegen"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// claudeInstructionsFile is the orientation file whose directory is a
// repository root when no .git or workbench marker is found first.
const claudeInstructionsFile = "CLAUDE.md"

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
	var mode codexgen.Mode
	switch args[0] {
	case "build":
		mode = codexgen.ModeBuild
	case "check":
		mode = codexgen.ModeCheck
	default:
		fmt.Fprintf(stderr, "engine-sync codex: unknown action %q\n", args[0])
		printCodexUsage(stderr)
		return 2
	}
	options, asJSON, code, ok := parseCodexOptions(args[0], args[1:], stdout, stderr, e)
	if !ok {
		return code
	}
	options.Mode = mode
	// A repository build also mirrors the home's global commands, the outputs
	// `global` writes: it holds both roots.
	code, err := guarded(e, []string{options.Root, globalRoot(options.Home)}, mode == codexgen.ModeBuild, func() int {
		return compileCodex(args[0], options, asJSON, stdout, stderr)
	})
	if err != nil {
		return reportRunFailure("engine-sync codex "+args[0], asJSON, err, stdout, stderr)
	}
	return code
}

// parseCodexOptions reads `codex build|check`'s flags and positional root.
func parseCodexOptions(action string, args []string, stdout, stderr io.Writer, e env) (codexgen.Options, bool, int, bool) {
	flags := cli.NewFlagSet("codex "+action, "usage: engine-sync codex "+action+" [repo-root] [options]", stderr)
	home := flags.String("home", "", "Codex global source/output home")
	var models, excludeDirs, excludeProjects, neverRegister repeatString
	flags.Var(&models, "model", "model alias mapping alias=value; repeatable")
	rootAdapter := flags.String("root-adapter", "", "replace the generated root AGENTS adapter")
	agentPreamble := flags.String("agent-preamble", "", "replace the generated agent preamble")
	flags.Var(&excludeDirs, "exclude-dir", "exclude a command directory; repeatable")
	flags.Var(&excludeProjects, "exclude-project", "exclude a project; repeatable")
	flags.Var(&neverRegister, "never-register", "do not register an agent; repeatable")
	suffixMode := flags.String("suffix-mode", "", "agent suffix mode")
	suffixPrefix := flags.String("suffix-prefix", "", "agent suffix prefix")
	asJSON := flags.Bool("json", false, "print one JSON result object instead of the report")
	positionals, code, ok := cli.ParseFlagsAnywhere(flags, args)
	if !ok {
		return codexgen.Options{}, false, code, false
	}
	if len(positionals) > 1 {
		flags.Usage()
		return codexgen.Options{}, false, 2, false
	}
	failed := func(what string, err error) (codexgen.Options, bool, int, bool) {
		return codexgen.Options{}, false, reportRunFailure("engine-sync codex "+action, *asJSON,
			fmt.Errorf("%s: %w", what, err), stdout, stderr), false
	}
	root, err := repoRoot(positionals)
	if err != nil {
		return failed("resolve repo root", err)
	}
	resolvedHome := *home
	if resolvedHome == "" {
		if resolvedHome, err = e.home(); err != nil {
			return failed("resolve home", err)
		}
	}
	if resolvedHome, err = filepath.Abs(resolvedHome); err != nil {
		return failed("resolve home", err)
	}
	overrides, err := codexCLIOverrides(
		models, *rootAdapter, *agentPreamble, excludeDirs, excludeProjects, neverRegister, *suffixMode, *suffixPrefix,
	)
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync codex %s: %v\n", action, err)
		return codexgen.Options{}, false, 2, false
	}
	flags.Visit(func(seen *flag.Flag) {
		switch seen.Name {
		case "root-adapter":
			overrides.SetRootAdapter = true
		case "agent-preamble":
			overrides.SetAgentPreamble = true
		}
	})
	return codexgen.Options{Root: root, Home: resolvedHome, CLIOverrides: overrides}, *asJSON, 0, true
}

func compileCodex(action string, options codexgen.Options, asJSON bool, stdout, stderr io.Writer) int {
	result, err := codexgen.Run(options)
	if asJSON {
		return opencodegen.WriteJSON(stdout, stderr, opencodegen.Result{
			OK: result.OK, Warnings: result.Warnings, Problems: result.Problems, Rebuildable: result.Rebuildable,
			Wrote: result.Wrote, Unchanged: result.Unchanged, Deleted: result.Deleted,
		}, err)
	}
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync codex %s: %v\n", action, err)
		return 1
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(stderr, "engine-sync codex: warning: %s\n", warning)
	}
	for _, problem := range result.Problems {
		fmt.Fprintf(stderr, "engine-sync codex: %s\n", problem)
	}
	if !result.OK {
		return 1
	}
	fmt.Fprintf(stdout, "CODEX %s PASS\n", strings.ToUpper(action))
	return 0
}

// reportRunFailure prints a failure the guard or the lock raised, in the
// shape the caller asked for, and returns exit 1.
func reportRunFailure(prefix string, asJSON bool, err error, stdout, stderr io.Writer) int {
	if asJSON {
		return opencodegen.WriteJSON(stdout, stderr, opencodegen.Result{}, err)
	}
	fmt.Fprintf(stderr, "%s: %v\n", prefix, err)
	return 1
}

func printCodexUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: engine-sync codex build|check|agents [repo-root] [options]")
	fmt.Fprintln(w, "  --home PATH")
	fmt.Fprintln(w, "  --model alias=value       repeatable")
	fmt.Fprintln(w, "  --root-adapter TEXT       replace root adapter")
	fmt.Fprintln(w, "  --agent-preamble TEXT     replace agent preamble")
	fmt.Fprintln(w, "  --exclude-dir NAME        repeatable")
	fmt.Fprintln(w, "  --exclude-project NAME    repeatable")
	fmt.Fprintln(w, "  --never-register NAME     repeatable")
	fmt.Fprintln(w, "  --suffix-mode MODE [--suffix-prefix TEXT]")
	fmt.Fprintln(w, "  --json                    one JSON result object on stdout")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage: engine-sync codex agents [--home PATH]")
	fmt.Fprintln(w, "  compiles every {home}/.professor/templates/global/agents/*.md, symlinks")
	fmt.Fprintln(w, "  {home}/.claude/agents to the .md sources, and writes {home}/.codex/agents/*.toml as")
	fmt.Fprintln(w, "  regular files — Codex refuses to load a role through a symlink.")
}

// runCodexAgents compiles the global (host-level) agents: no repository root,
// every source and destination anchored on --home.
func runCodexAgents(args []string, stdout, stderr io.Writer, e env) int {
	flags := cli.NewFlagSet("codex agents", "usage: engine-sync codex agents [--home PATH]", stderr)
	home := flags.String("home", "", "host HOME whose global agents get compiled and installed")
	positionals, code, ok := cli.ParseFlagsAnywhere(flags, args)
	if !ok {
		return code
	}
	if len(positionals) != 0 {
		flags.Usage()
		return 2
	}
	resolvedHome := *home
	var err error
	if resolvedHome == "" {
		if resolvedHome, err = e.home(); err != nil {
			fmt.Fprintf(stderr, "engine-sync codex agents: resolve home: %v\n", err)
			return 1
		}
	}
	if resolvedHome, err = filepath.Abs(resolvedHome); err != nil {
		fmt.Fprintf(stderr, "engine-sync codex agents: resolve home: %v\n", err)
		return 1
	}
	code, err = guarded(e, []string{globalRoot(resolvedHome)}, true, func() int {
		result, err := codexgen.RunGlobalAgents(codexgen.GlobalAgentsOptions{Home: resolvedHome})
		if err != nil {
			fmt.Fprintf(stderr, "engine-sync codex agents: %v\n", err)
			return 1
		}
		for _, compiled := range result.Compiled {
			fmt.Fprintf(stdout, "%s: %d B, parses clean\n", compiled.Path, compiled.Size)
		}
		for _, installed := range result.Installed {
			fmt.Fprintf(stdout, "%s %s\n", installed.State,
				codexgen.DescribeGlobalLinkState(installed.State, installed.Path, installed.Source, installed.Found))
		}
		for _, role := range result.Roles {
			fmt.Fprintf(stdout, "%s %s\n", role.State, role.Describe())
		}
		for _, problem := range result.Problems {
			fmt.Fprintf(stderr, "engine-sync codex agents: %s\n", problem)
		}
		fmt.Fprintln(stdout, "CODEX AGENTS PASS")
		return 0
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
func repoRoot(positionals []string) (string, error) {
	if len(positionals) == 1 {
		return filepath.Abs(positionals[0])
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	fallback := ""
	for {
		if found, err := paths.HasWorkbenchManifest(dir); err != nil {
			return "", err
		} else if found {
			return dir, nil
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		if fallback == "" {
			instructions := filepath.Join(dir, claudeInstructionsFile)
			if info, statErr := os.Stat(instructions); statErr == nil && info.Mode().IsRegular() {
				fallback = dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			if fallback != "" {
				return fallback, nil
			}
			return "", errors.New("no repository root found (expected .git or CLAUDE.md)")
		}
		dir = parent
	}
}

type repeatString []string

func (list *repeatString) String() string { return strings.Join(*list, ",") }
func (list *repeatString) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("repeated option value must be non-empty")
	}
	*list = append(*list, value)
	return nil
}

func codexCLIOverrides(
	models repeatString,
	adapter, preamble string,
	dirs, projects, agents repeatString,
	suffixMode, suffixPrefix string,
) (codexgen.CLIOverrides, error) {
	result := codexgen.CLIOverrides{
		RootAdapter:     adapter,
		AgentPreamble:   preamble,
		ExcludeDirs:     []string(dirs),
		ExcludeProjects: []string(projects),
		NeverRegister:   []string(agents),
		SuffixMode:      suffixMode,
		SuffixPrefix:    suffixPrefix,
	}
	modelMap := make(map[string]string, len(models))
	for _, item := range models {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return result, fmt.Errorf("--model requires alias=value, got %q", item)
		}
		modelMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	if len(modelMap) != 0 {
		result.ModelMap = modelMap
	}
	return result, nil
}

// runOpenCode is the OpenCode compiler's command, behind the same guard.
func runOpenCode(args []string, stdout, stderr io.Writer, e env) int {
	home, err := e.home()
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync opencode: resolve home: %v\n", err)
		return 1
	}
	command, code, ok := opencodegen.ParseCommand(args, func() (string, error) { return repoRoot(nil) }, home, stdout, stderr)
	if !ok {
		return code
	}
	code, err = guarded(e, []string{command.Root}, command.Mode == opencodegen.ModeBuild, func() int {
		return command.Execute(stdout, stderr)
	})
	if err != nil {
		return reportRunFailure("engine-sync opencode "+command.Action, command.JSON, err, stdout, stderr)
	}
	return code
}
