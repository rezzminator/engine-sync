package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/rezzminator/engine-sync/internal/machine"
	"github.com/rezzminator/engine-sync/internal/paths"
)

type Targets = machine.Targets

func readMachineTargets(e env, home string) (Targets, bool, error) {
	return machine.ReadTargets(e.get, home)
}

func runGlobal(args []string, stdout, stderr io.Writer, e env) int {
	flags := NewFlagSet("global", "usage: engine-sync global [--home H] [--source-repo R] [--claude-config-dir C]... "+
		"[--codex-home X]... [--no-codex-home] [--source-home S] [--plan] [--uninstall] [--json]", stderr)
	home := flags.String("home", "", "the home every default is anchored on")
	sourceRepo := flags.String("source-repo", "", "the source repository (default Claude config dir agents)")
	var claudeDirs, codexHomes repeatString
	flags.Var(&claudeDirs, "claude-config-dir", "a Claude config dir; repeatable")
	flags.Var(&codexHomes, "codex-home", "a Codex home; repeatable")
	noCodex := flags.Bool("no-codex-home", false, "no Codex home is configured")
	sourceHome := flags.String("source-home", "", "the home whose .claude/commands the global commands compile from")
	plan := flags.Bool("plan", false, "write nothing; list what an apply would do")
	uninstall := flags.Bool("uninstall", false, "retire every generated role and managed command")
	asJSON := flags.Bool("json", false, "print one JSON object")
	positionals, code, ok := ParseFlagsAnywhere(flags, args)
	if !ok {
		return code
	}
	if len(positionals) != 0 || (*noCodex && len(codexHomes) != 0) {
		flags.Usage()
		return 2
	}
	options, err := globalOptions(e, *home, *sourceRepo, *sourceHome, claudeDirs, codexHomes, *noCodex)
	if err == nil {
		options.Config, err = e.policy("", options.Home, stderr)
	}
	if err == nil {
		options.Plan, options.Uninstall = *plan, *uninstall
		if options.CodexHomes == nil && options.Config.Sources["machine.codexHomes"] != "default" {
			options.CodexHomes = options.Config.Machine.CodexHomes
		}
	}
	report := machine.Report{Err: err}
	if err == nil {
		var guardErr error
		_, guardErr = guarded(e, []string{globalRoot(options.Home)}, !options.Plan, func() int {
			report = machine.RunMachine(options)
			if report.Err != nil || !report.OK {
				return 1
			}
			return 0
		})
		if guardErr != nil {
			report.Err = errors.Join(report.Err, guardErr)
		}
	}
	return printGlobal(report, *asJSON, stdout, stderr)
}

func globalOptions(
	e env, home, sourceRepo, sourceHome string, claudeDirs, codexHomes repeatString, noCodex bool,
) (machine.Options, error) {
	resolvedHome, err := absoluteHome(home, e)
	if err != nil {
		return machine.Options{}, fmt.Errorf("resolve home: %w", err)
	}
	options := machine.Options{Home: resolvedHome, SourceRepo: sourceRepo, ClaudeConfigDirs: claudeDirs, SourceHome: sourceHome}
	targets, found, err := readMachineTargets(e, resolvedHome)
	if err != nil {
		return machine.Options{}, err
	}
	if found {
		if len(options.ClaudeConfigDirs) == 0 {
			options.ClaudeConfigDirs = targets.ClaudeConfigDirs
		}
		if options.SourceRepo == "" {
			options.SourceRepo = targets.SourceRepo
		}
	}
	if len(options.ClaudeConfigDirs) == 0 {
		options.ClaudeConfigDirs = []string{paths.ClaudeConfigDir(e.get, resolvedHome)}
	}
	switch {
	case noCodex:
		options.CodexHomes = []string{}
	case len(codexHomes) != 0:
		options.CodexHomes = codexHomes
	case found:
		options.CodexHomes = *targets.CodexHomes
	case filepath.IsAbs(e.get("CODEX_HOME")):
		options.CodexHomes = []string{e.get("CODEX_HOME")}
	}

	for _, path := range []*string{&options.SourceRepo, &options.SourceHome} {
		if *path == "" {
			continue
		}
		if *path, err = filepath.Abs(*path); err != nil {
			return machine.Options{}, fmt.Errorf("resolve %s: %w", *path, err)
		}
	}
	return options, nil
}

func printGlobal(report machine.Report, asJSON bool, stdout, stderr io.Writer) int {
	ok := report.OK && report.Err == nil
	errText := ""
	if report.Err != nil {
		errText = report.Err.Error()
	}
	if asJSON {
		events := report.Events
		if events == nil {
			events = []machine.Event{}
		}
		if err := json.NewEncoder(stdout).Encode(struct {
			OK     bool            `json:"ok"`
			Events []machine.Event `json:"events"`
			Error  string          `json:"error"`
		}{OK: ok, Events: events, Error: errText}); err != nil {
			fmt.Fprintf(stderr, "engine-sync global: write JSON: %v\n", err)
			return 1
		}
	} else {
		for _, event := range report.Events {
			verb := event.Verb
			if verb == machine.VerbRetire {
				verb = machine.VerbChange
			}
			fmt.Fprintf(stdout, "  %-7s %s\n", verb, event.Message)
		}
		if errText != "" {
			fmt.Fprintf(stderr, "engine-sync global: %s\n", errText)
		}
	}
	if !ok {
		return 1
	}
	return 0
}
