package cli

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/cliargs"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/machine"
	"io"
	"path/filepath"
)

func runMachineDoctor(args []string, stdout, stderr io.Writer, e env) int {
	flags := cliargs.NewFlagSet("doctor --machine", "usage: engine-sync doctor --machine [--home H] [--source-repo R] "+
		"[--claude-config-dir C] [--codex-home X]... [--claude-absent] [--capability NAME]... [--json]", stderr)
	flags.Bool("machine", true, "the machine rows")
	home := flags.String("home", "", "the home whose registries are classified")
	sourceRepo := flags.String("source-repo", "", "the source clone; absent is the no-clone state")
	claudeConfig := flags.String("claude-config-dir", "", "the Claude agents registry's config dir")
	var codexHomes cliargs.RepeatString
	flags.Var(&codexHomes, "codex-home", "a Codex home; repeatable")
	claudeAbsent := flags.Bool("claude-absent", false, "no Claude Code binary is installed")
	var capabilities cliargs.RepeatString
	flags.Var(&capabilities, "capability", "a host capability an agent's requires may name; repeatable")
	asJSON := flags.Bool("json", false, "print one JSON object")
	positionals, code, ok := cliargs.ParseFlagsAnywhere(flags, args)
	if !ok {
		return code
	}
	if len(positionals) != 0 {
		flags.Usage()
		return 2
	}
	resolvedHome, err := absoluteHome(*home, e)
	if err != nil {
		return machine.PrintDoctor(nil, nil, fmt.Errorf("engine-sync doctor: resolve home: %w", err), *asJSON, stdout, stderr)
	}
	policy, err := e.policy("", resolvedHome, stderr)
	if err == nil {
		policy, err = config.WithCapabilities(e.get, resolvedHome, policy, capabilities)
	}
	if err != nil {
		return machine.PrintDoctor(nil, nil, err, *asJSON, stdout, stderr)
	}
	var cds cliargs.RepeatString
	if *claudeConfig != "" {
		cds = append(cds, *claudeConfig)
	}
	options, err := globalOptions(e, resolvedHome, *sourceRepo, "", cds, codexHomes, false)
	if err != nil {
		return cliargs.ReportFailure("engine-sync doctor", *asJSON, err, stdout, stderr)
	}
	var rows []machine.Row
	_, guardErr := guarded(e, machine.GuardRoots(options.Home, options.CodexHomes, options.ClaudeConfigDirs), false, func() int {
		rows = machine.Doctor(machine.DoctorOptions{Config: policy, Home: resolvedHome, SourceRepo: options.SourceRepo, ClaudeConfigDirs: options.ClaudeConfigDirs, CodexHomes: options.CodexHomes, ClaudeAbsent: *claudeAbsent})
		return 0
	})
	if guardErr != nil {
		rows = append(rows, machine.Row{Level: machine.LevelFail, Message: guardErr.Error()})
	}
	return machine.PrintDoctor(rows, config.Describe(*policy), nil, *asJSON, stdout, stderr)
}

func absoluteHome(flagValue string, e env) (string, error) {
	home := flagValue
	if home == "" {
		var err error
		if home, err = e.home(); err != nil {
			return "", err
		}
	}
	return filepath.Abs(home)
}
