package machine

import (
	"io"

	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/engine"
	"github.com/rezzminator/engine-sync/internal/paths"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/target"
)

// ProjectCommands is the optional global-commands step a project run carries:
// the machine policy, its targets and writer-guard roots, then every target's
// commands inside guard (writes when mode builds). Each target's result merges
// into combined and, unless quiet, prints its problems and warnings to stderr;
// the exit is 1 when any target did not pass. An error is the step's own
// failure (policy, targets or guard), never a project result.
func ProjectCommands(get paths.Env, home string, mode reconciler.Mode, targets []target.Target,
	policy func(root, home string, stderr io.Writer) (*config.Config, error),
	guard func(roots []string, writes bool, body func() int) (int, error),
	combined *diag.Report, stderr io.Writer, quiet bool) error {
	machinePolicy, err := policy("", home, stderr)
	if err != nil {
		return err
	}
	roster, err := config.ResolveTargets(get, home, *machinePolicy, config.Targets{})
	if err != nil {
		return err
	}
	_, err = guard(GuardRoots(home, *roster.CodexHomes, roster.ClaudeConfigDirs), mode == reconciler.Build, func() int {
		for _, t := range targets {
			r, pass := engine.Commands(home, "", *machinePolicy, *roster.CodexHomes, false, mode, t.Render, t.Roots, t.Validate)
			combined.Merge(r, pass)
			if !quiet {
				diag.PrintLines(stderr, r.Problems, r.Warnings)
			}
		}
		if !combined.OK {
			return 1
		}
		return 0
	})
	return err
}
