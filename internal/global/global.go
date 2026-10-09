// Package global is `engine-sync global`: the machine-level writes pfm
// install's Codex steps performed — the global agents (Codex role files, the
// rendered variant sources, the Claude agent links), the retirement of every
// orphaned, undeclared or renamed generated role, and the global commands
// mirrored into {home}/.codex — reported as the installer's own transcript.
// A plan writes nothing and lists exactly what the apply would do.
package global

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rezzminator/engine-sync/internal/codexgen"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// The transcript verbs, the installer's own.
const (
	VerbChange   = "change"
	VerbRetire   = "retire"
	VerbOK       = "ok"
	VerbSkip     = "skip"
	VerbConflict = "conflict"
)

// retiredGlobalAgents names every global agent identity a template rename has
// retired; a future rename adds its OLD name here.
var retiredGlobalAgents = []string{"frr", "rr-super"}

// Event is one transcript line. Path is set on a retire: the file removed, or
// to be removed under a plan.
type Event struct {
	Verb    string `json:"verb"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// Options select the homes and the source clone. A nil CodexHomes means
// {Home}/.codex alone; a non-nil empty one means no Codex home at all.
type Options struct {
	Home             string
	SourceRepo       string
	ClaudeConfigDirs []string
	CodexHomes       []string
	// SourceHome is the home whose .claude/commands the global commands
	// compile from; empty is Home.
	SourceHome string
	Plan       bool
	Uninstall  bool
}

// Report is the run's transcript. Err is a failure that stopped a step; a
// conflict also clears OK without stopping a plan.
type Report struct {
	Events []Event
	Err    error
	OK     bool
}

type run struct {
	options Options
	report  Report
	failed  []error
}

func (r *run) add(verb, message, path string) {
	r.report.Events = append(r.report.Events, Event{Verb: verb, Message: message, Path: path})
}

func (r *run) ok(message string)   { r.add(VerbOK, message, "") }
func (r *run) skip(message string) { r.add(VerbSkip, message, "") }

// change lists one write and, under an apply, performs it.
func (r *run) change(message string, action func() error) error {
	r.add(VerbChange, message, "")
	if r.options.Plan || action == nil {
		return nil
	}
	return action()
}

// retire removes one file or link pfm's installer would have retired: a real
// directory is refused, an absent path is nothing to do.
func (r *run) retire(path, reason string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("refuse to retire directory %s", path)
	}
	r.add(VerbRetire, "retire "+path+" ("+reason+")", path)
	if r.options.Plan {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("retire %s: %w", path, err)
	}
	return nil
}

func (r *run) codexHomes() []string {
	if r.options.CodexHomes == nil {
		return []string{filepath.Join(r.options.Home, ".codex")}
	}
	return r.options.CodexHomes
}

func (r *run) claudeConfigDirs() []string {
	if len(r.options.ClaudeConfigDirs) == 0 {
		return []string{filepath.Join(r.options.Home, ".claude")}
	}
	return r.options.ClaudeConfigDirs
}

// Run performs (or plans) every global step and returns the transcript. Each
// step runs whatever an earlier one returned, as the installer deferred its
// failures; the first failure is Err, every failure is joined into it.
func Run(options Options) Report {
	r := &run{options: options}
	if strings.TrimSpace(options.Home) == "" {
		r.report.Err = errors.New("global: no home")
		return r.report
	}
	if strings.TrimSpace(r.options.SourceRepo) == "" {
		r.options.SourceRepo = filepath.Join(options.Home, ".professor")
	}
	steps := []func() error{r.retireRenamedCodexAgents, r.wireAgents, r.globalCommands}
	if options.Uninstall {
		steps = []func() error{r.globalCommands, r.unwireGeneratedAgents}
	}
	for _, step := range steps {
		if err := step(); err != nil {
			r.failed = append(r.failed, err)
		}
	}
	r.report.Err = errors.Join(r.failed...)
	r.report.OK = r.report.Err == nil
	for _, event := range r.report.Events {
		if event.Verb == VerbConflict {
			r.report.OK = false
		}
	}
	return r.report
}

// wireAgents is the installer's wireCodexAgents: the plan's actions listed,
// its conflicts reported and preserved, the orphans retired, then the build.
func (r *run) wireAgents() error {
	source := filepath.Join(r.options.SourceRepo, "templates", "global", "agents")
	if _, err := os.Stat(source); errors.Is(err, fs.ErrNotExist) {
		r.skip("Codex global agents source absent at " + source)
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect Codex global agents source %s: %w", source, err)
	}
	agents := codexgen.GlobalAgentsOptions{
		Home:             r.options.Home,
		SourceRepo:       r.options.SourceRepo,
		ClaudeConfigDirs: r.claudeConfigDirs(),
		CodexHomes:       r.codexHomes(),
		Mode:             codexgen.ModeCheck,
	}
	plan, err := codexgen.RunGlobalAgents(agents)
	if err != nil {
		return fmt.Errorf("plan pfm codex agents: %w", err)
	}
	for _, action := range plan.Actions {
		message := action.Kind + " " + action.Path
		if action.Target != "" {
			message += " -> " + action.Target
		}
		r.add(VerbChange, message, "")
	}
	for _, problem := range plan.Problems {
		r.skip(problem)
	}
	if err := r.retireOrphanCodexRoles(plan.Roles); err != nil {
		return err
	}
	if err := r.retireOrphanGlobalAgents(plan.Installed); err != nil {
		return err
	}
	if r.options.Plan {
		return nil
	}
	agents.Mode = codexgen.ModeBuild
	result, err := codexgen.RunGlobalAgents(agents)
	if err != nil {
		return fmt.Errorf("run pfm codex agents: %w", err)
	}
	r.ok(fmt.Sprintf("Codex global agents compiled=%d linked=%d", len(result.Compiled), len(result.Installed)))
	return nil
}

// retireOrphanCodexRoles retires every generated role in a Codex registry
// whose name no current source declares.
func (r *run) retireOrphanCodexRoles(roles []codexgen.GlobalRoleInstalled) error {
	keep := make(map[string]bool, len(roles))
	for _, role := range roles {
		keep[strings.TrimSuffix(filepath.Base(role.Path), ".toml")] = true
	}
	generated := paths.LegacyGeneratedCodexAgentsDir(r.options.Home)
	legacyDir := filepath.Join(r.options.SourceRepo, "templates", "global", "agents")
	for _, codexHome := range r.codexHomes() {
		if err := r.retireCodexRoles(filepath.Join(codexHome, "agents"), generated, legacyDir, keep); err != nil {
			return err
		}
	}
	return nil
}

// retireCodexRoles retires every role in registry not in keep that pfm or
// engine-sync provably wrote: a link into the retired generated store or the
// in-clone twin, or a regular file carrying the generated marker.
func (r *run) retireCodexRoles(registry, generated, legacyDir string, keep map[string]bool) error {
	entries, err := os.ReadDir(registry)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect Codex global agents registry %s: %w", registry, err)
	}
	for _, entry := range entries {
		if keep[strings.TrimSuffix(entry.Name(), ".toml")] {
			continue
		}
		path := filepath.Join(registry, entry.Name())
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect Codex global agent %s: %w", path, err)
		}
		owned := false
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read Codex global agent link %s: %w", path, err)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			target = filepath.Clean(target)
			owned = within(target, generated) || target == filepath.Clean(filepath.Join(legacyDir, entry.Name()))
		case info.Mode().IsRegular():
			content, err := codexgen.ReadGlobalRoleFile(path)
			if err != nil {
				return fmt.Errorf("inspect Codex global agent %s: %w", path, err)
			}
			owned = codexgen.GeneratedGlobalRole(content)
		}
		if !owned {
			continue
		}
		if err := r.retire(path, "retired generated Codex agent"); err != nil {
			return err
		}
	}
	return nil
}

// retireOrphanGlobalAgents retires, in every Claude agents registry, a link
// to a rendered variant no longer declared, then every rendered variant
// source nothing declares.
func (r *run) retireOrphanGlobalAgents(installed []codexgen.GlobalAgentInstalled) error {
	generatedDir := paths.GeneratedClaudeAgentsDir(r.options.Home)
	keep := make(map[string]bool, len(installed))
	for _, entry := range installed {
		if within(entry.Source, generatedDir) {
			keep[filepath.Base(entry.Source)] = true
		}
	}
	for _, config := range r.claudeConfigDirs() {
		registry := filepath.Join(config, "agents")
		entries, err := os.ReadDir(registry)
		if errors.Is(err, fs.ErrNotExist) {
			entries, err = nil, nil
		}
		if err != nil {
			return fmt.Errorf("inspect global agent registry %s: %w", registry, err)
		}
		for _, entry := range entries {
			path := filepath.Join(registry, entry.Name())
			info, err := os.Lstat(path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("inspect global agent %s: %w", path, err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("read global agent link %s: %w", path, err)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			target = filepath.Clean(target)
			if within(target, generatedDir) && !keep[filepath.Base(target)] {
				if err := r.retire(
					path,
					"retired global agent variant — "+entry.Name()+" is no longer declared in variants.json",
				); err != nil {
					return err
				}
			}
		}
	}
	generatedEntries, err := os.ReadDir(generatedDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect generated global agent variants %s: %w", generatedDir, err)
	}
	for _, entry := range generatedEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || keep[entry.Name()] {
			continue
		}
		if err := r.retire(filepath.Join(generatedDir, entry.Name()), "undeclared generated agent variant"); err != nil {
			return err
		}
	}
	return nil
}

// retireRenamedCodexAgents retires a renamed-away identity's generated role
// in every Codex home; a file of that name without the marker is preserved.
func (r *run) retireRenamedCodexAgents() error {
	for _, home := range r.codexHomes() {
		for _, name := range retiredGlobalAgents {
			path := filepath.Join(home, "agents", name+".toml")
			content, err := os.ReadFile(path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("read retired Codex agent %s: %w", path, err)
			}
			if !codexgen.GeneratedGlobalRole(content) {
				r.skip(path + " is not a generated agent; preserved")
				continue
			}
			if err := r.retire(path, "renamed global agent"); err != nil {
				return err
			}
		}
	}
	return nil
}

// globalCommands mirrors {source home}/.claude/commands into {home}/.codex
// (prompts and skills), then retires the retired explorer role's strays;
// uninstall compiles from an empty source, so it only ever retires.
func (r *run) globalCommands() error {
	if len(r.codexHomes()) == 0 && !r.options.Uninstall {
		r.skip("no Codex accounts configured — command mirror has nothing to write")
		r.skip("no Codex accounts configured — agent mirror has nothing to write")
		return nil
	}
	sourceHome := r.options.SourceHome
	if r.options.Uninstall {
		empty, err := os.MkdirTemp("", "engine-sync-uninstall-commands-")
		if err != nil {
			return fmt.Errorf("create empty Codex command source for uninstall: %w", err)
		}
		defer func() {
			if err := os.RemoveAll(empty); err != nil {
				r.failed = append(r.failed, fmt.Errorf("remove temporary uninstall command source %s: %w", empty, err))
			}
		}()
		sourceHome = empty
	}
	commands := codexgen.GlobalCommandsOptions{Home: r.options.Home, SourceHome: sourceHome, Mode: codexgen.ModeCheck}
	plan, err := codexgen.RunGlobalCommands(commands)
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
			return r.retireExplorerStrays()
		}
		return fmt.Errorf("reconcile Codex global commands: %s", strings.Join(blockers, "; "))
	}
	if !r.options.Plan {
		commands.Mode = codexgen.ModeBuild
		result, err := codexgen.RunGlobalCommands(commands)
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
	return r.retireExplorerStrays()
}

// planBlockers keeps the problems a build cannot repair: a missing, stale or
// orphaned mirror file is what the build is for.
func planBlockers(problems []string) []string {
	blockers := make([]string, 0)
	for _, problem := range problems {
		if strings.HasPrefix(problem, "MISSING ") || strings.HasPrefix(problem, "STALE ") ||
			strings.HasPrefix(problem, "ORPHAN ") {
			continue
		}
		blockers = append(blockers, problem)
	}
	return blockers
}

// retireExplorerStrays retires the retired explorer role's compiled TOML and
// any timestamped backup an earlier copy-based install wrote beside it.
func (r *run) retireExplorerStrays() error {
	target := filepath.Join(r.options.Home, ".codex", "agents", "explorer.toml")
	if err := r.retire(target, "retired explorer agent"); err != nil {
		return err
	}
	matches, err := filepath.Glob(target + ".bak-*")
	if err != nil {
		return fmt.Errorf("glob retired explorer agent backups: %w", err)
	}
	for _, match := range matches {
		if err := r.retire(match, "retired explorer agent backup"); err != nil {
			return err
		}
	}
	return nil
}

// unwireGeneratedAgents is uninstall's half: the rendered variant store, every
// generated role in every Codex home, and the retired generated role store.
func (r *run) unwireGeneratedAgents() error {
	if err := r.removeDir(paths.GeneratedClaudeAgentsDir(r.options.Home), "generated Claude agents directory"); err != nil {
		return err
	}
	generated := paths.LegacyGeneratedCodexAgentsDir(r.options.Home)
	legacyDir := filepath.Join(r.options.SourceRepo, "templates", "global", "agents")
	for _, codexHome := range r.codexHomes() {
		if err := r.retireCodexRoles(filepath.Join(codexHome, "agents"), generated, legacyDir, nil); err != nil {
			return err
		}
	}
	return r.removeDir(generated, "generated Codex agents directory")
}

func (r *run) removeDir(dir, label string) error {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect %s %s: %w", label, dir, err)
	}
	return r.change("remove "+dir, func() error {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s %s: %w", label, dir, err)
		}
		return nil
	})
}

func within(target, dir string) bool {
	return target == dir || strings.HasPrefix(target, dir+string(filepath.Separator))
}
