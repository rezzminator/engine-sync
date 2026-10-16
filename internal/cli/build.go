package cli

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/cliargs"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/engine"
	"github.com/rezzminator/engine-sync/internal/machine"
	"github.com/rezzminator/engine-sync/internal/pluginlog"
	"github.com/rezzminator/engine-sync/internal/reconcile"
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
		pluginlog.Record(e.get, stderr, pluginlog.Row{Level: "info", Event: "usage-refused", Msg: action + ": incompatible --dry-run or --engine", Where: "internal/cli/build.go#runProjectTargets"})
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
		pluginlog.Record(e.get, stderr, pluginlog.Row{Level: "info", Event: "usage-refused", Msg: fmt.Sprintf("unknown engine %q", selected.Engine), Where: "internal/cli/build.go#runProjectTargets"})
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

	if !p.Workbench && cfg.Codex.GlobalCommands {
		machinePolicy, loadErr := e.policy("", o.Home, stderr)
		if loadErr != nil {
			return cliargs.ReportFailure("engine-sync "+action, asJSON, loadErr, stdout, stderr)
		}
		roster, loadErr := config.ResolveTargets(e.get, o.Home, *machinePolicy, config.Targets{})
		if loadErr != nil {
			return cliargs.ReportFailure("engine-sync "+action, asJSON, loadErr, stdout, stderr)
		}
		_, loadErr = guarded(e, machine.GuardRoots(o.Home, *roster.CodexHomes, roster.ClaudeConfigDirs), mode == reconcile.Build, func() int {
			for _, t := range targets {
				r, pass := engine.Commands(o.Home, "", *machinePolicy, *roster.CodexHomes, false, mode, t.Render, t.Roots, t.Validate)
				combined.Merge(r, pass)
				if !asJSON {
					diag.PrintLines(stderr, r.Problems, r.Warnings)
				}
			}
			if !combined.OK {
				return 1
			}
			return 0
		})
		if loadErr != nil {
			return cliargs.ReportFailure("engine-sync "+action, asJSON, loadErr, stdout, stderr)
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
	pluginlog.RecordOutcome(e.get, stderr, pluginlog.ProjectOutcome{Action: action, Code: code, Applied: mode == reconcile.Build, Warnings: combined.Warnings, Problems: combined.Problems, Error: err, Session: e.sessionID})
	if asJSON {
		if mode == reconcile.Plan {
			return diag.WritePlan(stdout, stderr, combined, combined.Actions, err, code)
		}
		if action == "doctor" {
			return diag.WriteJSON(stdout, stderr, combined, err, config.Describe(*cfg))
		}
		return diag.WriteJSON(stdout, stderr, combined, err)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if action == "doctor" {
		for _, row := range config.Describe(*cfg) {
			fmt.Fprintf(stdout, "CONFIG %s=%s (%s)\n", row.Key, row.Value, row.Source)
		}
	}
	return code
}
