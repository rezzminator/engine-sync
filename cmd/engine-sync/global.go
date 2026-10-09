package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rezzminator/engine-sync/internal/cli"
	"github.com/rezzminator/engine-sync/internal/global"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// Targets is {engine-sync config}/targets.json: the machine's Codex homes,
// and optionally the Claude config dirs and the source clone.
type Targets struct {
	CodexHomes       *[]string `json:"codexHomes"`
	ClaudeConfigDirs []string  `json:"claudeConfigDirs,omitempty"`
	SourceRepo       string    `json:"sourceRepo,omitempty"`
}

// readTargets returns the targets file, found false when there is none. A
// file that is present but unreadable, unparseable or without codexHomes is
// an error, never an empty roster.
func readTargets(e env, home string) (Targets, bool, error) {
	path := filepath.Join(paths.EngineSyncConfig(e.get, home), "targets.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Targets{}, false, nil
	}
	if err != nil {
		return Targets{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var targets Targets
	if err := json.Unmarshal(raw, &targets); err != nil {
		return Targets{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	if targets.CodexHomes == nil {
		return Targets{}, false, fmt.Errorf("%s: no \"codexHomes\" list", path)
	}
	for _, dir := range append(append([]string(nil), *targets.CodexHomes...), targets.ClaudeConfigDirs...) {
		if !filepath.IsAbs(dir) {
			return Targets{}, false, fmt.Errorf("%s: %q is not an absolute path", path, dir)
		}
	}
	return targets, true, nil
}

func runGlobal(args []string, stdout, stderr io.Writer, e env) int {
	flags := cli.NewFlagSet("global", "usage: engine-sync global [--home H] [--source-repo R] [--claude-config-dir C]... "+
		"[--codex-home X]... [--no-codex-home] [--source-home S] [--plan] [--uninstall] [--json]", stderr)
	home := flags.String("home", "", "the home every default is anchored on")
	sourceRepo := flags.String("source-repo", "", "the source clone (default {home}/.professor)")
	var claudeDirs, codexHomes repeatString
	flags.Var(&claudeDirs, "claude-config-dir", "a Claude config dir; repeatable")
	flags.Var(&codexHomes, "codex-home", "a Codex home; repeatable")
	noCodex := flags.Bool("no-codex-home", false, "no Codex home is configured")
	sourceHome := flags.String("source-home", "", "the home whose .claude/commands the global commands compile from")
	plan := flags.Bool("plan", false, "write nothing; list what an apply would do")
	uninstall := flags.Bool("uninstall", false, "retire every generated role and managed command")
	asJSON := flags.Bool("json", false, "print one JSON object")
	positionals, code, ok := cli.ParseFlagsAnywhere(flags, args)
	if !ok {
		return code
	}
	if len(positionals) != 0 || (*noCodex && len(codexHomes) != 0) {
		flags.Usage()
		return 2
	}
	options, err := globalOptions(e, *home, *sourceRepo, *sourceHome, claudeDirs, codexHomes, *noCodex)
	if err == nil {
		options.Plan, options.Uninstall = *plan, *uninstall
	}
	report := global.Report{Err: err}
	if err == nil {
		var guardErr error
		_, guardErr = guarded(e, []string{globalRoot(options.Home)}, !options.Plan, func() int {
			report = global.Run(options)
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
) (global.Options, error) {
	resolvedHome, err := absoluteHome(home, e)
	if err != nil {
		return global.Options{}, fmt.Errorf("resolve home: %w", err)
	}
	options := global.Options{Home: resolvedHome, SourceRepo: sourceRepo, ClaudeConfigDirs: claudeDirs, SourceHome: sourceHome}
	switch {
	case noCodex:
		options.CodexHomes = []string{}
	case len(codexHomes) != 0:
		options.CodexHomes = codexHomes
	default:
		targets, found, err := readTargets(e, resolvedHome)
		if err != nil {
			return global.Options{}, err
		}
		switch {
		case found:
			options.CodexHomes = *targets.CodexHomes
			if len(options.ClaudeConfigDirs) == 0 {
				options.ClaudeConfigDirs = targets.ClaudeConfigDirs
			}
			if options.SourceRepo == "" {
				options.SourceRepo = targets.SourceRepo
			}
		case filepath.IsAbs(e.get("CODEX_HOME")):
			options.CodexHomes = []string{e.get("CODEX_HOME")}
		}
	}
	for _, path := range []*string{&options.SourceRepo, &options.SourceHome} {
		if *path == "" {
			continue
		}
		if *path, err = filepath.Abs(*path); err != nil {
			return global.Options{}, fmt.Errorf("resolve %s: %w", *path, err)
		}
	}
	return options, nil
}

func printGlobal(report global.Report, asJSON bool, stdout, stderr io.Writer) int {
	ok := report.OK && report.Err == nil
	errText := ""
	if report.Err != nil {
		errText = report.Err.Error()
	}
	if asJSON {
		events := report.Events
		if events == nil {
			events = []global.Event{}
		}
		if err := json.NewEncoder(stdout).Encode(struct {
			OK     bool           `json:"ok"`
			Events []global.Event `json:"events"`
			Error  string         `json:"error"`
		}{OK: ok, Events: events, Error: errText}); err != nil {
			fmt.Fprintf(stderr, "engine-sync global: write JSON: %v\n", err)
			return 1
		}
	} else {
		for _, event := range report.Events {
			verb := event.Verb
			if verb == global.VerbRetire {
				verb = global.VerbChange
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
