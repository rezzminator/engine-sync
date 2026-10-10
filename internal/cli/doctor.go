package cli

import (
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/machine"
	"io"
	"path/filepath"
)

func runMachineDoctor(args []string, stdout, stderr io.Writer, e env) int {
	flags := NewFlagSet("doctor --machine", "usage: engine-sync doctor --machine [--home H] [--source-repo R] "+
		"[--claude-config-dir C] [--codex-home X]... [--claude-absent] [--json]", stderr)
	flags.Bool("machine", true, "the machine rows")
	home := flags.String("home", "", "the home whose registries are classified")
	sourceRepo := flags.String("source-repo", "", "the source clone; absent is the no-clone state")
	claudeConfig := flags.String("claude-config-dir", "", "the Claude agents registry's config dir")
	var codexHomes repeatString
	flags.Var(&codexHomes, "codex-home", "a Codex home; repeatable")
	claudeAbsent := flags.Bool("claude-absent", false, "no Claude Code binary is installed")
	asJSON := flags.Bool("json", false, "print one JSON object")
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
		fmt.Fprintf(stderr, "engine-sync doctor: resolve home: %v\n", err)
		return 1
	}
	policy, err := e.policy("", resolvedHome, stderr)
	if err != nil {
		if *asJSON {
			_ = json.NewEncoder(stdout).Encode(map[string]any{"ok": false, "rows": []machine.Row{}, "configSources": []config.SourceRow{}, "error": err.Error()})
		} else {
			fmt.Fprintln(stderr, err)
		}
		return 1
	}
	rows := machine.Doctor(machine.DoctorOptions{
		Config: policy, Home: resolvedHome, SourceRepo: *sourceRepo, ClaudeConfigDir: *claudeConfig,
		CodexHomes: []string(codexHomes), ClaudeAbsent: *claudeAbsent,
	})
	failed := false
	for _, row := range rows {
		failed = failed || row.Level == machine.LevelFail
	}
	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(struct {
			OK            bool               `json:"ok"`
			Rows          []machine.Row      `json:"rows"`
			ConfigSources []config.SourceRow `json:"configSources"`
		}{OK: !failed, Rows: rows, ConfigSources: config.Describe(*policy)}); err != nil {
			fmt.Fprintf(stderr, "engine-sync doctor: write JSON: %v\n", err)
			return 1
		}
	} else {
		for _, row := range config.Describe(*policy) {
			fmt.Fprintf(stdout, "CONFIG %s=%s (%s)\n", row.Key, row.Value, row.Source)
		}
		for _, row := range rows {
			fmt.Fprintf(stdout, "%s %s\n", row.Level, row.Message)
		}
	}
	if failed {
		return 1
	}
	return 0
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
