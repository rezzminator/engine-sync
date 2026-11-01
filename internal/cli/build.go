package cli

import (
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/cliargs"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/engine"
	"github.com/rezzminator/engine-sync/internal/machine"
	"github.com/rezzminator/engine-sync/internal/pluginlog"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/runlog"
	"github.com/rezzminator/engine-sync/internal/target"
	"io"
)

func runProjectTargets(action string, args []string, stdout, stderr io.Writer, e env, filter string) int {
	selected := cliargs.Selection{Engine: filter}
	o, asJSON, code, ok := parseCodexOptions(action, args, stdout, stderr, e, &selected)
	if !ok {
		return code
	}
	if (selected.DryRun && action != "build") || (filter != "" && filter != selected.Engine) {
		runlog.Record(e.get, stderr, pluginlog.Row{Level: "info", Event: "usage-refused", Msg: action + ": incompatible --dry-run or --engine", Where: "internal/cli/build.go#runProjectTargets"})
		fmt.Fprintf(stderr, "engine-sync %s: incompatible --dry-run or --engine\n", action)
		return 2
	}
	var targets []target.Target
	for _, t := range target.Registry {
		if selected.Engine == "" || selected.Engine == t.Name() {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		runlog.Record(e.get, stderr, pluginlog.Row{Level: "info", Event: "usage-refused", Msg: fmt.Sprintf("unknown engine %q", selected.Engine), Where: "internal/cli/build.go#runProjectTargets"})
		fmt.Fprintf(stderr, "engine-sync: unknown engine %q\n", selected.Engine)
		return 2
	}
	cfg, err := e.policy(o.Root, o.Home, stderr)
	if err == nil {
		var value config.Config
		value, err = cfg.WithCodexOverrides(o.CLIOverrides)
		cfg = &value
	}
	if err == nil {
		cfg, err = config.WithCapabilities(e.get, o.Home, cfg, o.Capabilities)
	}
	if err != nil {
		return cliargs.ReportFailure("engine-sync "+action, asJSON, err, stdout, stderr)
	}
	p := e.source(o.Root, o.Home, *cfg)
	mode := reconcile.Check
	if action == "build" {
		mode = reconcile.Build
	}
	if action == "plan" || selected.DryRun {
		mode = reconcile.Plan
	}
	roots := []string{o.Root}
	combined := diag.Report{OK: true}

	// The global-commands step is optional: its failure (a machine policy or
	// targets that do not resolve, a global root the writer guard refuses) is a
	// problem of this run, never a reason to skip the project the user named.
	var globalErr error
	if !p.Workbench && cfg.Codex.GlobalCommands {
		globalErr = machine.ProjectCommands(e.get, o.Home, mode, targets, e.policy,
			func(r []string, w bool, body func() int) (int, error) { return guarded(e, r, w, body) }, &combined, stderr, asJSON)
		if globalErr != nil && !asJSON {
			fmt.Fprintln(stderr, "engine-sync "+action+": global commands: "+globalErr.Error())
		}
	}
	if action == "check" || action == "doctor" {
		problems, warnings := machine.CheckSourceDeclarations(o.Root, o.Home, *cfg)
		combined.Merge(reconcile.Result{Problems: problems, Warnings: warnings}, len(problems) == 0)
		if !asJSON {
			diag.PrintLines(stderr, problems, warnings)
		}
	}
	code, err = guarded(e, roots, mode == reconcile.Build, func() int {
		for _, t := range targets {
			r, pass := engine.Execute(*p, *cfg, mode, t.Render, t.Roots, t.Validate)
			if mode == reconcile.Plan {
				repair := map[string]bool{}
				for _, s := range r.Rebuildable {
					repair[s] = true
				}
				pass = true
				for _, s := range r.Problems {
					if !repair[s] {
						pass = false
					}
				}
			}
			combined.Merge(r, pass)
			if !asJSON {
				diag.PrintEngine(stdout, stderr, r, mode, t.Name(), action, pass)
			}
		}
		if !combined.OK {
			return 1
		}
		return 0
	})
	globalCode := 0
	if globalErr != nil {
		combined.Merge(reconcile.Result{Problems: []string{"global commands: " + globalErr.Error()}}, false)
		if code == 0 && err == nil {
			// The project passed: the exit is the global step's own, so a busy
			// lock still reads as busy (75) and a refusal as a refusal (2).
			globalCode = cliargs.FailureCode(globalErr)
			code = globalCode
		}
	}
	runlog.RecordOutcome(e.get, stderr, runlog.ProjectOutcome{Action: action, Code: code, Applied: mode == reconcile.Build, Warnings: combined.Warnings, Problems: combined.Problems, Error: errors.Join(err, globalErr), Session: e.sessionID})
	if asJSON {
		if mode == reconcile.Plan {
			return diag.WritePlan(stdout, stderr, combined, combined.Actions, err, code)
		}
		if action == "doctor" {
			return max(diag.WriteJSON(stdout, stderr, combined, err, config.Describe(*cfg)), globalCode)
		}
		return max(diag.WriteJSON(stdout, stderr, combined, err), globalCode)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return cliargs.FailureCode(err)
	}
	if action == "doctor" {
		for _, row := range config.Describe(*cfg) {
			fmt.Fprintf(stdout, "CONFIG %s=%s (%s)\n", row.Key, row.Value, row.Source)
		}
	}
	return code
}
