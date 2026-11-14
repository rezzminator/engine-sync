package diag

import (
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"io"
	"strings"
)

func PrintEngine(stdout, stderr io.Writer, r reconcile.Result, mode reconcile.Mode, name, action string, pass bool) {

	for _, w := range r.Warnings {
		fmt.Fprintf(stderr, "engine-sync %s: warning: %s\n", name, w)
	}
	if mode != reconcile.Plan || !pass {
		for _, p := range r.Problems {
			fmt.Fprintf(stderr, "engine-sync %s: %s\n", name, p)
		}
	}
	if mode == reconcile.Build && name == "opencode" {
		for _, a := range r.Actions {
			if a.Kind == "delete" {
				fmt.Fprintf(stdout, "engine-sync opencode: deleted %s\n", a.Path)
			}
		}
	}
	if mode == reconcile.Plan {
		for _, a := range r.Actions {
			fmt.Fprintf(stdout, "%s %s %s\n", a.Kind, a.Path, a.Target)
		}
	} else if pass {
		if mode == reconcile.Build && name == "opencode" {
			fmt.Fprintf(stdout, "OPENCODE BUILD PASS wrote=%d unchanged=%d deleted=%d\n", r.Wrote, r.Unchanged, r.Deleted)
		} else {
			fmt.Fprintf(stdout, "%s %s PASS\n", strings.ToUpper(name), strings.ToUpper(action))
		}
	}

}

// Skips prints the row of each engine $ENGINE_SYNC_DISABLE left alone. When
// all is set no engine is left to run and Skips answers the run, done true:
// exit 0 and, with asJSON, the one passing object naming them.
func Skips(stdout, stderr io.Writer, skipped []string, all bool, action string, asJSON, plan bool) (code int, done bool) {
	if !asJSON {
		for _, name := range skipped {
			fmt.Fprintf(stdout, "%s %s SKIP disabled by %s\n", strings.ToUpper(name), strings.ToUpper(action), paths.EnvDisable)
		}
	}
	if !all || len(skipped) == 0 || !asJSON {
		return 0, all && len(skipped) > 0
	}
	report := Report{OK: true, Skipped: skipped}
	if plan {
		return WritePlan(stdout, stderr, report, nil, nil, 0), true
	}
	return WriteJSON(stdout, stderr, report, nil), true
}

func WritePlan(stdout, stderr io.Writer, report Report, actions []reconcile.Action, err error, code int) int {

	record := planJSON{OK: report.OK && err == nil, Problems: report.Problems, Rebuildable: report.Rebuildable, Warnings: report.Warnings, Actions: make([]planAction, 0, len(actions)), Skipped: report.Skipped}
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
	Skipped     []string     `json:"skipped,omitempty"`
}
