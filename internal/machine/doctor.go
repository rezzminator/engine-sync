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
	"sort"
	"strings"

	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/target/codex"
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
	default:
		return line + ` hint="run engine-sync global"`
	}
}

// Level is a status's row level: a registry missing, unreadable, symlinked or
// mismatched fails; source and conflict states warn.
func (status AgentsStatus) Level() string {
	switch status.State {
	case AgentsLinked, AgentsInstalled, AgentsNoClaude:
		return LevelOK
	case AgentsMissing, AgentsUnreadable, AgentsSymlink, AgentsMismatch:
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
		plan := RunMachine(Options{Config: options.Config, Home: options.Home, SourceRepo: options.SourceRepo, ClaudeConfigDirs: options.ClaudeConfigDirs, CodexHomes: options.CodexHomes, Plan: true})
		for _, event := range plan.Events {
			if event.Verb == VerbChange || event.Verb == VerbRetire || event.Verb == VerbConflict {
				rows = append(rows, Row{Level: LevelFail, Message: event.Message})
			}
		}
		if plan.Err != nil {
			rows = append(rows, Row{Level: LevelFail, Message: plan.Err.Error()})
		}
	}

	failed := false
	for _, row := range rows {
		failed = failed || row.Level == LevelFail
	}
	if failed {
		filtered := rows[:0]
		for _, row := range rows {
			if row.Level != LevelOK {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	return rows
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
		return []AgentsStatus{{Dir: config.MachinePath(home, paths.Getenv), State: AgentsUnreadable, Error: configErr.Error()}}
	}
	agentsDir := source.AgentsDirectory(home, repo, *cfg)
	entries, err := os.ReadDir(agentsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return []AgentsStatus{{Dir: agentsDir, State: AgentsUnreadable, Error: err.Error()}}
	}
	var sources []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".md") && !strings.EqualFold(entry.Name(), "README.md") {
			sources = append(sources, filepath.Join(agentsDir, entry.Name()))
		}
	}
	if len(sources) == 0 {
		return []AgentsStatus{{Dir: agentsDir, State: AgentsNoSources}}
	}
	sort.Strings(sources)
	variants, err := source.LoadGlobalAgentVariants(agentsDir, filepath.Join(home, cfg.Machine.GeneratedAgentsDir), cfg.Machine.Variants, cfg.Machine.AgentMarkerDir)
	if err != nil {
		return []AgentsStatus{{Dir: agentsDir, State: AgentsUnreadable, Error: err.Error()}}
	}
	for _, variant := range variants {
		sources = append(sources, variant.Path)
	}
	dirs := options.ClaudeConfigDirs
	if len(dirs) == 0 {
		dirs = []string{store}
	}
	statuses := []AgentsStatus{}
	for _, dir := range dirs {
		statuses = append(statuses, inspectStore(dir, paths.GlobalSourceRoot(home, repo, cfg.ClaudeConfigDir), sources))
	}
	roles, err := CompileGlobalRoles(home, repo, cfg)
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
		found, err := codex.ReadGlobalRoleFile(target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			missing = append(missing, role.Name)
		case errors.Is(err, codex.ErrGlobalRoleNotRegular) && isSymlink(target):
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
		state, _, err := codex.ClassifyGlobalLink(
			filepath.Join(registry, filepath.Base(source)), source, repo, codex.GlobalLinkFile,
		)
		if err != nil {
			status.State = AgentsUnreadable
			status.Error = err.Error()
			return status
		}
		switch state {
		case codex.GlobalLinkCorrect:
		case codex.GlobalLinkConflict:
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
