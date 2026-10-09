package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/rezzminator/engine-sync/internal/cli"
	"github.com/rezzminator/engine-sync/internal/global"
	"github.com/rezzminator/engine-sync/internal/opencodegen"
)

// runBoth is `engine-sync build|check`: the Codex command, then the OpenCode
// one, each printing its own report; both run whatever the first returned.
// The codex options all pass to the Codex command; only --home, --json and
// the root reach OpenCode.
func runBoth(action string, args []string, stdout, stderr io.Writer, e env) int {
	return runPair(args, stdout, stderr,
		func(out, errOut io.Writer) int { return runCodex(append([]string{action}, args...), out, errOut, e) },
		func(out, errOut io.Writer) int {
			return runOpenCode(append([]string{action}, openCodeArgs(args)...), out, errOut, e)
		})
}

// runPair runs the Codex half, then the OpenCode half. Under --json their two
// replies fold into the one object the interface promises: ok when both are,
// every list joined, the counts summed, both errors named; a half that printed
// no reply (a usage error) passes both halves' output through unfolded.
func runPair(args []string, stdout, stderr io.Writer, codex, opencode func(out, errOut io.Writer) int) int {
	if !jsonRequested(args) {
		return worst(codex(stdout, stderr), opencode(stdout, stderr))
	}
	var codexOut, openOut bytes.Buffer
	codexCode := codex(&codexOut, stderr)
	openCode := opencode(&openOut, stderr)
	var first, second opencodegen.JSONResult
	if json.Unmarshal(codexOut.Bytes(), &first) != nil || json.Unmarshal(openOut.Bytes(), &second) != nil ||
		codexCode > 1 || openCode > 1 {
		if _, err := io.Copy(stdout, io.MultiReader(&codexOut, &openOut)); err != nil {
			fmt.Fprintf(stderr, "engine-sync: write output: %v\n", err)
		}
		return worst(codexCode, openCode, 1)
	}
	var runErr error
	for _, text := range []string{first.Error, second.Error} {
		if text != "" {
			runErr = errors.Join(runErr, errors.New(text))
		}
	}
	return opencodegen.WriteJSON(stdout, stderr, opencodegen.Result{
		OK:          first.OK && second.OK,
		Problems:    append(first.Problems, second.Problems...),
		Rebuildable: append(first.Rebuildable, second.Rebuildable...),
		Warnings:    append(first.Warnings, second.Warnings...),
		Wrote:       first.Wrote + second.Wrote,
		Unchanged:   first.Unchanged + second.Unchanged,
		Deleted:     first.Deleted + second.Deleted,
	}, runErr)
}

// jsonRequested reports a --json flag among args, in any spelling the flag
// parser accepts as true.
func jsonRequested(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--json", "-json", "--json=true", "-json=true", "--json=1", "-json=1":
			return true
		}
	}
	return false
}

// runDoctor is the repository doctor (codex check, then opencode doctor) or,
// with --machine, the machine rows.
func runDoctor(args []string, stdout, stderr io.Writer, e env) int {
	for _, arg := range args {
		if arg == "--machine" || arg == "-machine" {
			return runMachineDoctor(args, stdout, stderr, e)
		}
	}
	return runPair(args, stdout, stderr,
		func(out, errOut io.Writer) int { return runCodex(append([]string{"check"}, args...), out, errOut, e) },
		func(out, errOut io.Writer) int {
			return runOpenCode(append([]string{"doctor"}, openCodeArgs(args)...), out, errOut, e)
		})
}

// worst is the combined exit: a usage error wins, then a finding.
func worst(codes ...int) int {
	out := 0
	for _, code := range codes {
		if code > out {
			out = code
		}
	}
	return out
}

// openCodeArgs keeps the arguments the OpenCode command reads: positionals,
// --home and --json; every Codex-only option (and its value) is dropped.
func openCodeArgs(args []string) []string {
	valued := map[string]bool{
		"--model": true, "--root-adapter": true, "--agent-preamble": true, "--exclude-dir": true,
		"--exclude-project": true, "--never-register": true, "--suffix-mode": true, "--suffix-prefix": true,
	}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := arg
		if eq := indexByte(arg, '='); eq > 0 {
			name = arg[:eq]
		}
		name = "--" + trimDashes(name)
		switch {
		case len(arg) > 0 && arg[0] != '-':
			out = append(out, arg)
		case valued[name]:
			if indexByte(arg, '=') < 0 {
				i++
			}
		case name == "--home":
			out = append(out, arg)
			if indexByte(arg, '=') < 0 && i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
		default:
			out = append(out, arg)
		}
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimDashes(s string) string {
	for len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}
	return s
}

func runMachineDoctor(args []string, stdout, stderr io.Writer, e env) int {
	flags := cli.NewFlagSet("doctor --machine", "usage: engine-sync doctor --machine [--home H] [--source-repo R] "+
		"[--claude-config-dir C] [--codex-home X]... [--claude-absent] [--json]", stderr)
	flags.Bool("machine", true, "the machine rows")
	home := flags.String("home", "", "the home whose registries are classified")
	sourceRepo := flags.String("source-repo", "", "the source clone; absent is the no-clone state")
	claudeConfig := flags.String("claude-config-dir", "", "the Claude agents registry's config dir")
	var codexHomes repeatString
	flags.Var(&codexHomes, "codex-home", "a Codex home; repeatable")
	claudeAbsent := flags.Bool("claude-absent", false, "no Claude Code binary is installed")
	asJSON := flags.Bool("json", false, "print one JSON object")
	positionals, code, ok := cli.ParseFlagsAnywhere(flags, args)
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
	rows := global.Doctor(global.DoctorOptions{
		Home: resolvedHome, SourceRepo: *sourceRepo, ClaudeConfigDir: *claudeConfig,
		CodexHomes: []string(codexHomes), ClaudeAbsent: *claudeAbsent,
	})
	failed := false
	for _, row := range rows {
		failed = failed || row.Level == global.LevelFail
	}
	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(struct {
			OK   bool         `json:"ok"`
			Rows []global.Row `json:"rows"`
		}{OK: !failed, Rows: rows}); err != nil {
			fmt.Fprintf(stderr, "engine-sync doctor: write JSON: %v\n", err)
			return 1
		}
	} else {
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
