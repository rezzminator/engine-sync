package cliargs

import (
	"errors"
	"flag"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"github.com/rezzminator/engine-sync/internal/target/opencode"
	"io"
	"path/filepath"
	"strings"
)

type Selection struct {
	Engine string
	DryRun bool
}

func ParseProjectOptions(action string, args []string, stdout, stderr io.Writer, rootPath func([]string) (string, error), homePath func() (string, error), selections ...*Selection) (codex.Options, bool, int, bool) {
	usage := "usage: engine-sync " + action + " [repo-root] [options]"
	if action == "doctor" {
		usage += "\nusage: engine-sync doctor --machine [--home H] [--source-repo R] [--codex-home X]... [--json]"
	}
	flags := NewFlagSet(action, usage, stderr)
	home := flags.String("home", "", "machine source/output home")
	var models, excludeDirs, excludeProjects, neverRegister RepeatString
	flags.Var(&models, "model", "model alias mapping alias=value; repeatable")
	rootAdapter := flags.String("root-adapter", "", "replace the generated root AGENTS adapter")
	agentPreamble := flags.String("agent-preamble", "", "replace the generated agent preamble")
	flags.Var(&excludeDirs, "exclude-dir", "exclude a command directory; repeatable")
	flags.Var(&excludeProjects, "exclude-project", "exclude a project; repeatable")
	flags.Var(&neverRegister, "never-register", "do not register an agent; repeatable")
	suffixMode := flags.String("suffix-mode", "", "agent suffix mode")
	suffixPrefix := flags.String("suffix-prefix", "", "agent suffix prefix")
	choice := &Selection{}
	if len(selections) > 0 {
		choice = selections[0]
	}
	flags.StringVar(&choice.Engine, "engine", choice.Engine, "target name from registry")
	flags.BoolVar(&choice.DryRun, "dry-run", false, "plan without writes")
	var capabilities RepeatString
	flags.Var(&capabilities, "capability", "a host capability an agent's requires may name; repeatable")
	asJSON := flags.Bool("json", false, "print one JSON result object instead of the report")
	positionals, code, ok := ParseFlagsAnywhere(flags, args)
	if !ok {
		return codex.Options{}, false, code, false
	}
	if len(positionals) > 1 {
		flags.Usage()
		return codex.Options{}, false, 2, false
	}
	failed := func(what string, err error) (codex.Options, bool, int, bool) {
		return codex.Options{}, false, ReportFailure("engine-sync codex "+action, *asJSON,
			fmt.Errorf("%s: %w", what, err), stdout, stderr), false
	}
	root, err := rootPath(positionals)
	if err != nil {
		return failed("resolve repo root", err)
	}
	resolvedHome := *home
	if resolvedHome == "" {
		if resolvedHome, err = homePath(); err != nil {
			return failed("resolve home", err)
		}
	}
	if resolvedHome, err = filepath.Abs(resolvedHome); err != nil {
		return failed("resolve home", err)
	}
	overrides, err := projectOverrides(
		models, *rootAdapter, *agentPreamble, excludeDirs, excludeProjects, neverRegister, *suffixMode, *suffixPrefix,
	)
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync codex %s: %v\n", action, err)
		return codex.Options{}, false, 2, false
	}
	flags.Visit(func(seen *flag.Flag) {
		switch seen.Name {
		case "root-adapter":
			overrides.SetRootAdapter = true
		case "agent-preamble":
			overrides.SetAgentPreamble = true
		}
	})
	return codex.Options{Root: root, Home: resolvedHome, CLIOverrides: overrides, Capabilities: []string(capabilities)}, *asJSON, 0, true
}

func ReportFailure(prefix string, asJSON bool, err error, stdout, stderr io.Writer) int {
	if asJSON {
		return diag.WriteJSON(stdout, stderr, opencode.Result{}, err)
	}
	fmt.Fprintf(stderr, "%s: %v\n", prefix, err)
	return 1
}

func CodexUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: engine-sync codex build|check|agents [repo-root] [options]")
	fmt.Fprintln(w, "  --home PATH")
	fmt.Fprintln(w, "  --model alias=value       repeatable")
	fmt.Fprintln(w, "  --root-adapter TEXT       replace root adapter")
	fmt.Fprintln(w, "  --agent-preamble TEXT     replace agent preamble")
	fmt.Fprintln(w, "  --exclude-dir NAME        repeatable")
	fmt.Fprintln(w, "  --exclude-project NAME    repeatable")
	fmt.Fprintln(w, "  --never-register NAME     repeatable")
	fmt.Fprintln(w, "  --capability NAME         a host capability an agent's requires may name; repeatable")
	fmt.Fprintln(w, "  --suffix-mode MODE [--suffix-prefix TEXT]")
	fmt.Fprintln(w, "  --json                    one JSON result object on stdout")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage: engine-sync codex agents [--home PATH] [--source-repo PATH] [--capability NAME]...")
	fmt.Fprintln(w, "  compiles every {Claude config dir}/agents/*.md, symlinks")
	fmt.Fprintln(w, "  {home}/.claude/agents to the .md sources, and writes {home}/.codex/agents/*.toml as")
	fmt.Fprintln(w, "  regular files — Codex refuses to load a role through a symlink.")
}

func projectOverrides(
	models RepeatString,
	adapter, preamble string,
	dirs, projects, agents RepeatString,
	suffixMode, suffixPrefix string,
) (codex.CLIOverrides, error) {
	result := codex.CLIOverrides{
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

type RepeatString []string

func (list *RepeatString) String() string { return strings.Join(*list, ",") }
func (list *RepeatString) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("repeated option value must be non-empty")
	}
	*list = append(*list, value)
	return nil
}
