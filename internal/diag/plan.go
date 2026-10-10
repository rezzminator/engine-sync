package diag

import (
	"encoding/json"
	"fmt"
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
func WritePlan(stdout, stderr io.Writer, report Report, actions []reconcile.Action, err error, code int) int {

	record := planJSON{OK: report.OK && err == nil, Problems: report.Problems, Rebuildable: report.Rebuildable, Warnings: report.Warnings, Actions: make([]planAction, 0, len(actions))}
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
}
