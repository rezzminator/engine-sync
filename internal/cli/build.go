package cli

import (
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/engine"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/target"
	"io"
	"strings"
)

type selection struct {
	engine string
	dryRun bool
}

func runProjectTargets(action string, args []string, stdout, stderr io.Writer, e env, filter string) int {
	selected := selection{engine: filter}
	parseAction := action
	if action == "doctor" || action == "plan" {
		parseAction = "check"
	}
	o, asJSON, code, ok := parseCodexOptions(parseAction, args, stdout, stderr, e, &selected)
	if !ok {
		return code
	}
	var targets []target.Target
	for _, t := range target.Registry {
		if selected.engine == "" || selected.engine == t.Name() {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		fmt.Fprintf(stderr, "engine-sync: unknown engine %q\n", selected.engine)
		return 2
	}
	cfg, err := e.policy(o.Root, o.Home, stderr)
	if err == nil {
		var value config.Config
		value, err = cfg.WithCodexOverrides(o.CLIOverrides)
		cfg = &value
	}
	if err != nil {
		return reportRunFailure("engine-sync "+action, asJSON, err, stdout, stderr)
	}
	p := e.source(o.Root, o.Home, *cfg)
	mode := reconcile.Check
	if action == "build" {
		mode = reconcile.Build
	}
	if action == "plan" || selected.dryRun {
		mode = reconcile.Plan
	}
	roots := []string{o.Root}
	for _, t := range targets {
		if t.Name() == "codex" {
			roots = append(roots, globalRoot(o.Home))
			break
		}
	}
	var combined diag.Report
	combined.OK = true
	var actions []reconcile.Action
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
			combined.OK = combined.OK && pass
			combined.Problems = append(combined.Problems, r.Problems...)
			combined.Rebuildable = append(combined.Rebuildable, r.Rebuildable...)
			combined.Warnings = append(combined.Warnings, r.Warnings...)
			combined.Wrote += r.Wrote
			combined.Unchanged += r.Unchanged
			combined.Deleted += r.Deleted
			actions = append(actions, r.Actions...)
			if !asJSON {
				for _, w := range r.Warnings {
					fmt.Fprintf(stderr, "engine-sync %s: warning: %s\n", t.Name(), w)
				}
				if mode != reconcile.Plan {
					for _, p := range r.Problems {
						fmt.Fprintf(stderr, "engine-sync %s: %s\n", t.Name(), p)
					}
				}
				if mode == reconcile.Plan {
					for _, a := range r.Actions {
						fmt.Fprintf(stdout, "%s %s %s\n", a.Kind, a.Path, a.Target)
					}
				} else if pass {
					if mode == reconcile.Build && t.Name() == "opencode" {
						fmt.Fprintf(stdout, "OPENCODE BUILD PASS wrote=%d unchanged=%d deleted=%d\n", r.Wrote, r.Unchanged, r.Deleted)
					} else {
						fmt.Fprintf(stdout, "%s %s PASS\n", strings.ToUpper(t.Name()), strings.ToUpper(action))
					}
				}
			}
		}
		if !combined.OK {
			return 1
		}
		return 0
	})
	if asJSON {
		if mode == reconcile.Plan {
			record := planJSON{OK: combined.OK && err == nil, Problems: combined.Problems, Rebuildable: combined.Rebuildable, Warnings: combined.Warnings, Actions: make([]planAction, 0, len(actions))}
			if record.Problems == nil {
				record.Problems = []string{}
			}
			if record.Rebuildable == nil {
				record.Rebuildable = []string{}
			}
			if record.Warnings == nil {
				record.Warnings = []string{}
			}
			if err != nil {
				record.Error = err.Error()
			}
			for _, a := range actions {
				record.Actions = append(record.Actions, planAction{a.Kind, a.Path, a.Target})
			}
			if encodeErr := json.NewEncoder(stdout).Encode(record); encodeErr != nil {
				fmt.Fprintln(stderr, encodeErr)
				return 1
			}
			return code
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

type planAction struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Target string `json:"target,omitempty"`
}
type planJSON struct {
	OK          bool         `json:"ok"`
	Problems    []string     `json:"problems"`
	Rebuildable []string     `json:"rebuildable"`
	Warnings    []string     `json:"warnings"`
	Wrote       int          `json:"wrote"`
	Unchanged   int          `json:"unchanged"`
	Deleted     int          `json:"deleted"`
	Error       string       `json:"error"`
	Actions     []planAction `json:"actions"`
}

var _ artifact.Kind
