package machine

import (
	"bytes"
	"errors"
	"fmt"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// AgentsState is what one agents registry holds, measured against the agent
// sources the clone ships. UNREADABLE is its own state on purpose: a registry
// that could not be read is "we failed to look", never MISSING's "nothing
// there".
type AgentsState string

// The registry states, as engine-sync doctor printed them.
const (
	AgentsLinked     AgentsState = "linked"
	AgentsMissing    AgentsState = AgentsState(reconciler.Missing)
	AgentsConflict   AgentsState = AgentsState(reconciler.Conflict)
	AgentsUnreadable AgentsState = "UNREADABLE"
	AgentsNoSources  AgentsState = "NO-SOURCES"
	AgentsNoClaude   AgentsState = "NO-CLAUDE"
	AgentsInstalled  AgentsState = "installed"
	AgentsSymlink    AgentsState = "SYMLINK"
	AgentsMismatch   AgentsState = "MISMATCH"
	AgentsBroken     AgentsState = "BROKEN"
	// AgentsOrphan: a rendered agent in the generated directory whose
	// declaration source is gone.
	AgentsOrphan AgentsState = AgentsState(reconciler.Orphan)
)

// The row levels.
const (
	LevelOK   = "ok"
	LevelWarn = "warn"
	LevelFail = "fail"
)

// AgentsStatus is one registry's classification.
type AgentsStatus struct {
	// Store marks the Claude agents registry; otherwise it is a Codex one.
	Store     bool
	Dir       string
	State     AgentsState
	Names     []string
	Missing   []string
	Conflicts []string
	Error     string
}

// Row is one doctor line.
type Row struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// DoctorOptions select what the machine doctor classifies. An empty
// SourceRepo is the no-clone state, the clone a first install would use being
// {Home}/.claude.
type DoctorOptions struct {
	Config           *config.Config
	Home             string
	SourceRepo       string
	ClaudeConfigDirs []string
	ClaudeConfigDir  string
	CodexHomes       []string
	ClaudeAbsent     bool
}

// Describe renders the one operator-facing line of a status; every
// non-clean state carries its remediation.
func (status AgentsStatus) Describe() string {
	scope := "source=" + status.Dir
	if status.Store {
		scope = "store dir=" + status.Dir
	}
	line := fmt.Sprintf("%s state=%s", scope, status.State)
	if len(status.Names) != 0 {
		line += " names=" + strings.Join(status.Names, ",")
	}
	if len(status.Missing) != 0 {
		line += " missing=" + strings.Join(status.Missing, ",")
	}
	if len(status.Conflicts) != 0 {
		line += " conflict=" + strings.Join(status.Conflicts, ",")
	}
	if status.Error != "" {
		line += " error=" + status.Error
	}
	switch status.State {
	case AgentsLinked, AgentsInstalled:
		return line
	case AgentsSymlink:
		return line + ` note="Codex refuses a symlinked role and reports only 'agent type is currently not available'"` +
			` hint="run engine-sync global"`
	case AgentsNoClaude:
		return line + ` note="no Claude Code binary installed — the installer never wires the store's agents"`
	case AgentsBroken:
		return line + ` note="engine-sync global skips each named agent and keeps its installed copies"` +
			` hint="fix the named sources"`
	case AgentsOrphan:
		return line + ` note="each named file was rendered from a declaration that no longer exists"` +
			` hint="restore its declaration, or delete it (engine-sync global retires it)"`
	default:
		return line + ` hint="run engine-sync global"`
	}
}

// Level is a status's row level: a registry missing, unreadable, symlinked or
// mismatched fails, as does a broken source agent; source and conflict
// states warn.
func (status AgentsStatus) Level() string {
	switch status.State {
	case AgentsLinked, AgentsInstalled, AgentsNoClaude:
		return LevelOK
	case AgentsMissing, AgentsUnreadable, AgentsSymlink, AgentsMismatch, AgentsBroken, AgentsOrphan:
		return LevelFail
	default:
		return LevelWarn
	}
}

// Doctor classifies the Claude agents registry and each Codex registry and
// returns one row per registry, worded `global-agents {Describe}`.
func Doctor(options DoctorOptions) []Row {
	statuses := InspectRegistries(options)
	rows := make([]Row, 0, len(statuses))
	for _, status := range statuses {
		rows = append(rows, Row{Level: status.Level(), Message: "global-agents " + status.Describe()})
	}
	if !options.ClaudeAbsent {
		rows = append(rows, agentRows(options)...)
	}

	failed := false
	for _, row := range rows {
		failed = failed || row.Level == LevelFail
	}
	if failed {
		// A failing doctor hides its passing rows, never a SKIPPED one: an
		// agent left out by its requires is news on a failing host too.
		filtered := rows[:0]
		for _, row := range rows {
			if row.Level != LevelOK || strings.HasPrefix(row.Message, "agent "+StateSkipped+" ") {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	return rows
}

// agentRows are the per-agent rows: each agent's state, then each plan event
// no state row already names — a STALE agent is one row, never also its
// planned retirement. A config that fails to load is reported, and no
// directory is scanned from it: the scan is a NOT SCANNED row, never clean.
func agentRows(options DoctorOptions) []Row {
	cfg := options.Config
	if cfg == nil {
		resolved, err := config.LoadConfigLayers("", options.Home)
		if err != nil {
			return []Row{
				{Level: LevelFail, Message: "global-agents states: " + err.Error()},
				{Level: LevelFail, Message: "global-agents states NOT SCANNED — the config did not load, so no agent or generated directory is known"},
			}
		}
		cfg = &resolved
	}
	var rows []Row
	states, err := AgentStates(options.Home, options.SourceRepo, cfg, options.ClaudeConfigDirs, options.CodexHomes)
	if err != nil {
		rows = append(rows, Row{Level: LevelFail, Message: "global-agents states: " + err.Error()})
	}
	stale := map[string]bool{}
	for _, state := range states {
		rows = append(rows, Row{Level: state.Level(), Message: state.Describe()})
		if state.State == StateStale || state.State == StateOrphan {
			stale[state.Path] = true
		}
	}
	plan := RunMachine(Options{Config: options.Config, Home: options.Home, SourceRepo: options.SourceRepo, ClaudeConfigDirs: options.ClaudeConfigDirs, CodexHomes: options.CodexHomes, Plan: true})
	for _, event := range plan.Events {
		if (event.Verb == VerbChange || event.Verb == VerbRetire || event.Verb == VerbConflict) && !namesStale(event.Message, stale) {
			if orphan, ok := markedPromptOrphan(event); ok {
				rows = append(rows, Row{Level: orphan.Level(), Message: orphan.Describe()})
				continue
			}
			rows = append(rows, Row{Level: LevelFail, Message: event.Message})
		}
	}
	if plan.Err != nil {
		rows = append(rows, Row{Level: LevelFail, Message: plan.Err.Error()})
	}
	return rows
}

// markedPromptOrphan reads a planned deletion of a Codex prompt or an OpenCode
// command: one carrying engine-sync's header and a managed-by line is an
// ORPHAN state naming the installer; any other file keeps its plan row. The
// file is read here, never trusted from the event.
func markedPromptOrphan(event Event) (AgentState, bool) {
	path, ok := strings.CutPrefix(event.Message, "delete ")
	if !ok || !strings.HasSuffix(path, ".md") {
		return AgentState{}, false
	}
	engine := EngineOpenCode
	if filepath.Base(filepath.Dir(path)) == "prompts" {
		engine = EngineCodex
	}
	header, err := paths.ReadHeader(path)
	if err != nil {
		return AgentState{}, false
	}
	value, ok := artifact.ManagedBy(string(header))
	if !ok {
		return AgentState{}, false
	}
	return AgentState{Engine: engine, Name: strings.TrimSuffix(filepath.Base(path), ".md"), Path: path, State: StateOrphan, Detail: "managed-by " + value}, true
}

// namesStale reports whether a plan event's message ("{action} {path} …")
// acts on a path a STALE row already names.
func namesStale(message string, stale map[string]bool) bool {
	_, rest, ok := strings.Cut(message, " ")
	if !ok {
		return false
	}
	for path := range stale {
		if rest == path || strings.HasPrefix(rest, path+" ") {
			return true
		}
	}
	return false
}

// Inspect classifies every registry against the source clone.
func InspectRegistries(options DoctorOptions) []AgentsStatus {
	home := options.Home
	store := options.ClaudeConfigDir
	if store == "" {
		store = paths.ClaudeConfigDir(paths.Getenv, home)
	}
	if options.ClaudeAbsent {
		return []AgentsStatus{{Store: true, Dir: store, State: AgentsNoClaude}}
	}
	repo := options.SourceRepo
	cfg := options.Config
	var configErr error
	if cfg == nil {
		resolved, err := config.LoadConfigLayers("", home)
		configErr = err
		cfg = &resolved
	}
	if configErr != nil {
		return []AgentsStatus{{Dir: config.MachinePath(home), State: AgentsUnreadable, Error: configErr.Error()}}
	}
	dirs := options.ClaudeConfigDirs
	if len(dirs) == 0 {
		dirs = []string{store}
	}
	agentsDir := source.AgentsDirectory(home, repo, *cfg)
	model, err := source.LoadMachine(home, agentsDir, source.GeneratedDirectory(home, repo, *cfg), source.MachineOptions{MarkerDir: cfg.Machine.AgentMarkerDir, ClaudeConfigDirs: dirs})
	if err != nil {
		return []AgentsStatus{{Dir: agentsDir, State: AgentsUnreadable, Error: err.Error()}}
	}
	if len(model.Warnings) != 0 {
		return []AgentsStatus{{Dir: agentsDir, State: AgentsUnreadable, Error: strings.Join(model.Warnings, "; ")}}
	}
	orphans, err := generatedOrphans(model)
	if err != nil {
		return []AgentsStatus{{Dir: model.GeneratedDir, State: AgentsUnreadable, Error: err.Error()}}
	}
	// A broken agent is global's to skip, never the directory's to fail: it
	// is one BROKEN row naming it, and every healthy agent is still inspected.
	model, _ = filterMachineAgents(model, cfg.Capabilities)
	model = skipBrokenAgents(model, cfg)
	var broken []AgentsStatus
	if len(model.Problems) != 0 || len(model.Failed) != 0 {
		broken = []AgentsStatus{{Dir: agentsDir, State: AgentsBroken, Names: model.Failed, Error: strings.Join(model.Problems, "; ")}}
	}
	if len(orphans) != 0 {
		broken = append(broken, AgentsStatus{Dir: model.GeneratedDir, State: AgentsOrphan, Names: orphans})
	}
	var sources []string
	for _, agent := range model.Agents {
		sources = append(sources, agent.Path)
	}
	if len(sources) == 0 {
		return append(broken, AgentsStatus{Dir: agentsDir, State: AgentsNoSources})
	}
	statuses := broken
	for _, dir := range dirs {
		statuses = append(statuses, inspectStore(dir, paths.GlobalSourceRoot(home, repo, cfg.ClaudeConfigDir), sources))
	}
	roles, err := CompileGlobalRoles(home, repo, dirs, cfg)
	if err != nil {
		return append(statuses, AgentsStatus{Dir: agentsDir, State: AgentsUnreadable, Error: err.Error()})
	}
	codexHomes := options.CodexHomes
	if codexHomes == nil {
		codexHomes = []string{filepath.Join(home, ".codex")}
	}
	for _, codexHome := range codexHomes {
		statuses = append(statuses, inspectCodexRegistry(filepath.Join(codexHome, "agents"), roles))
	}
	return statuses
}

func inspectCodexRegistry(registry string, roles []GlobalRole) AgentsStatus {
	status := AgentsStatus{Dir: registry, State: AgentsInstalled}
	var symlinked, mismatched, missing, unreadable []string
	for _, role := range roles {
		target := filepath.Join(registry, role.Name+".toml")
		found, err := reconciler.ReadGlobalRoleFile(target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			missing = append(missing, role.Name)
		case errors.Is(err, reconciler.ErrGlobalRoleNotRegular) && isSymlink(target):
			symlinked = append(symlinked, role.Name)
		case err != nil:
			unreadable = append(unreadable, role.Name)
			if status.Error == "" {
				status.Error = err.Error()
			}
		case !bytes.Equal(found, role.Content):
			mismatched = append(mismatched, role.Name)
		}
	}
	switch {
	case len(unreadable) != 0:
		status.State = AgentsUnreadable
		status.Names = unreadable
		status.Missing = missing
		status.Conflicts = append(append([]string(nil), symlinked...), mismatched...)
	case len(symlinked) != 0:
		status.State = AgentsSymlink
		status.Names = symlinked
		status.Missing = missing
		status.Conflicts = mismatched
	case len(mismatched) != 0:
		status.State = AgentsMismatch
		status.Names = mismatched
		status.Missing = missing
	case len(missing) != 0:
		status.State = AgentsMissing
		status.Names = missing
	}
	return status
}

func inspectStore(store, repo string, sources []string) AgentsStatus {
	status := AgentsStatus{Store: true, Dir: store, State: AgentsLinked}
	registry := filepath.Join(store, "agents")
	var conflicting, missing []string
	for _, source := range sources {
		same, e := paths.SameEntry(filepath.Join(registry, filepath.Base(source)), source)
		if e != nil {
			status.State = AgentsUnreadable
			status.Error = e.Error()
			return status
		}
		if same {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(source), ".md")
		state, _, err := reconciler.ClassifyGlobalLink(
			filepath.Join(registry, filepath.Base(source)), source, repo, reconciler.GlobalLinkFile,
		)
		if err != nil {
			status.State = AgentsUnreadable
			status.Error = err.Error()
			return status
		}
		switch state {
		case reconciler.GlobalLinkCorrect:
		case reconciler.GlobalLinkConflict:
			conflicting = append(conflicting, name)
		default:
			// Missing, a copy where a link belongs, and a stale in-repo link
			// are three ways of not having this agent; one rerun fixes all.
			missing = append(missing, name)
		}
	}
	switch {
	case len(conflicting) != 0:
		status.State = AgentsConflict
		status.Names = conflicting
		status.Missing = missing
	case len(missing) != 0:
		status.State = AgentsMissing
		status.Names = missing
	}
	return status
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// generatedOrphans names each rendered file in the model's generated
// directory that no declaration renders any more, a capability-skipped or
// failed declaration still counting as declared. A directory that is absent
// has none; one that cannot be read is an error, never an empty list.
func generatedOrphans(model source.Project) ([]string, error) {
	if model.GeneratedDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(model.GeneratedDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read generated agents %s: %w", model.GeneratedDir, err)
	}
	declared := map[string]bool{}
	for _, agent := range model.Agents {
		if agent.Generated {
			declared[agent.Name] = true
		}
	}
	for _, name := range model.Failed {
		declared[name] = true
	}
	var orphans []string
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".md")
		if ok && !entry.IsDir() && !declared[name] {
			orphans = append(orphans, filepath.Join(model.GeneratedDir, entry.Name()))
		}
	}
	return orphans, nil
}
