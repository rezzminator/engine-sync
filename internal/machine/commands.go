package machine

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/engine"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"github.com/rezzminator/engine-sync/internal/target/opencode"
	"path/filepath"
	"strings"
)

func (r *run) globalCommands() error {
	if len(r.codexHomes()) == 0 && !r.options.Uninstall {
		r.skip("no Codex accounts configured — command mirror has nothing to write")
		r.skip("no Codex accounts configured — agent mirror has nothing to write")
		return nil
	}
	sourceHome := r.options.SourceHome

	homes := r.codexHomes()
	if r.options.Uninstall && len(homes) == 0 {
		homes = []string{filepath.Join(r.options.Home, ".codex")}
	}
	commands := codex.GlobalCommandsOptions{Config: &r.policy, Home: r.options.Home, SourceHome: sourceHome, Uninstall: r.options.Uninstall, CodexHomes: homes, Mode: codex.ModeCheck}
	plan, err := codex.RunGlobalCommands(commands)
	if err != nil {
		return fmt.Errorf("plan Codex global commands: %w", err)
	}
	for _, action := range plan.Actions {
		message := action.Kind + " " + action.Path
		if action.Target != "" {
			message += " -> " + action.Target
		}
		r.add(VerbChange, message, "")
	}
	if blockers := planBlockers(plan.Problems); len(blockers) != 0 {
		for _, blocker := range blockers {
			r.add(VerbConflict, blocker, "")
		}
		if r.options.Plan {
			// The conflict events clear OK; the plan goes on listing.
			return r.retireConfiguredFiles()
		}
		return fmt.Errorf("reconcile Codex global commands: %s", strings.Join(blockers, "; "))
	}
	if !r.options.Plan {
		commands.Mode = codex.ModeBuild
		result, err := codex.RunGlobalCommands(commands)
		if err != nil {
			return fmt.Errorf("reconcile Codex global commands: %w", err)
		}
		if !result.OK {
			return fmt.Errorf("reconcile Codex global commands: %s", strings.Join(result.Problems, "; "))
		}
		r.ok(fmt.Sprintf(
			"Codex global commands wrote=%d unchanged=%d deleted=%d", result.Wrote, result.Unchanged, result.Deleted,
		))
	}
	if r.options.Uninstall {
		return nil
	}
	return r.retireConfiguredFiles()
}

// planBlockers keeps the problems a build cannot repair: a missing, stale or
// orphaned mirror file is what the build is for.
func planBlockers(problems []string) []string {
	blockers := make([]string, 0)
	for _, problem := range problems {
		if strings.HasPrefix(problem, string(reconciler.Missing)+" ") || strings.HasPrefix(problem, string(reconciler.Stale)+" ") ||
			strings.HasPrefix(problem, string(reconciler.Orphan)+" ") {
			continue
		}
		blockers = append(blockers, problem)
	}
	return blockers
}

// openCodeCommands reconciles the shared command destination under machine policy.
func (r *run) openCodeCommands() error {
	t := opencode.Target{}
	mode := reconciler.Build
	if r.options.Plan {
		mode = reconciler.Plan
	}
	result, ok := engine.Commands(r.options.Home, r.options.SourceHome, r.policy, r.codexHomes(), r.options.Uninstall, mode, t.Render, t.Roots, t.Validate)
	for _, a := range result.Actions {
		r.add(VerbChange, a.Kind+" "+a.Path, "")
	}
	for _, s := range result.Warnings {
		r.skip(s)
	}
	blockers := planBlockers(result.Problems)
	if (!ok && !r.options.Plan) || len(blockers) > 0 {
		return fmt.Errorf("OpenCode commands: %s", strings.Join(result.Problems, "; "))
	}
	return nil
}
