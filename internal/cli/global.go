package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/machine"
)

type Targets = config.Targets

func runGlobal(args []string, stdout, stderr io.Writer, e env) int {
	flags := NewFlagSet("global", "usage: engine-sync global [--home H] [--source-repo R] [--claude-config-dir C]... "+
		"[--codex-home X]... [--no-codex-home] [--source-home S] [--plan] [--uninstall] [--adopt] [--capability NAME]... [--json]", stderr)
	home := flags.String("home", "", "the home every default is anchored on")
	sourceRepo := flags.String("source-repo", "", "the source repository (default Claude config dir agents)")
	var claudeDirs, codexHomes repeatString
	flags.Var(&claudeDirs, "claude-config-dir", "a Claude config dir; repeatable")
	flags.Var(&codexHomes, "codex-home", "a Codex home; repeatable")
	noCodex := flags.Bool("no-codex-home", false, "no Codex home is configured")
	sourceHome := flags.String("source-home", "", "the home whose .claude/commands the global commands compile from")
	plan := flags.Bool("plan", false, "write nothing; list what an apply would do")
	uninstall := flags.Bool("uninstall", false, "retire every generated role and managed command")
	adopt := flags.Bool("adopt", false, "move an unowned file at a declared agent's path aside, then replace it")
	var capabilities repeatString
	flags.Var(&capabilities, "capability", "a host capability an agent's requires may name; repeatable")
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
		if *adopt {
			options.AdoptDir = machine.AdoptDir(e.state(options.Home), time.Now())
		}
		if options.CodexHomes == nil && options.Config.Sources["machine.codexHomes"] != "default" {
			options.CodexHomes = options.Config.Machine.CodexHomes
		}
	}
	report := machine.Report{Err: err}
	if err == nil {
		var guardErr error
		_, guardErr = guarded(e, machine.GuardRoots(options.Home, options.CodexHomes, options.ClaudeConfigDirs), !options.Plan, func() int {
			// The capability set is read under the writer lock: a run that
			// waited for another never acts on a targets.json read before it.
			withCapabilities, capErr := config.WithCapabilities(e.get, options.Home, options.Config, capabilities)
			if capErr != nil {
				report = machine.Report{Err: capErr}
				return 1
			}
			options.Config = withCapabilities
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
	policy, err := e.policy("", resolvedHome, io.Discard)
	if err != nil {
		return machine.Options{}, err
	}
	options.Config = policy
	override := Targets{SourceRepo: sourceRepo, ClaudeConfigDirs: claudeDirs}
	if noCodex {
		v := []string{}
		override.CodexHomes = &v
	} else if len(codexHomes) > 0 {
		v := []string(codexHomes)
		override.CodexHomes = &v
	}
	resolved, err := config.ResolveTargets(e.get, resolvedHome, *policy, override)
	if err != nil {
		return machine.Options{}, err
	}
	options.SourceRepo, options.ClaudeConfigDirs, options.CodexHomes = resolved.SourceRepo, resolved.ClaudeConfigDirs, *resolved.CodexHomes

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

var printGlobal = machine.PrintReport
