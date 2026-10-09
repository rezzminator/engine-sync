package global

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rezzminator/engine-sync/internal/codexgen"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// AgentsState is what one agents registry holds, measured against the agent
// sources the clone ships. UNREADABLE is its own state on purpose: a registry
// that could not be read is "we failed to look", never MISSING's "nothing
// there".
type AgentsState string

// The registry states, as pfm doctor printed them.
const (
	AgentsLinked     AgentsState = "linked"
	AgentsMissing    AgentsState = "MISSING"
	AgentsConflict   AgentsState = "CONFLICT"
	AgentsUnreadable AgentsState = "UNREADABLE"
	AgentsNoSources  AgentsState = "NO-SOURCES"
	AgentsNoClone    AgentsState = "NO-CLONE"
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
// {Home}/.professor.
type DoctorOptions struct {
	Home            string
	SourceRepo      string
	ClaudeConfigDir string
	CodexHomes      []string
	ClaudeAbsent    bool
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
			` hint="run pfm install --yes"`
	case AgentsNoClone:
		return line + ` note="no Professor clone recorded or at the default path — global agents install from a clone (INSTALL.md § Build from source)"`
	case AgentsNoClaude:
		return line + ` note="no Claude Code binary installed — the installer never wires the store's agents"`
	default:
		return line + ` hint="run pfm install"`
	}
}

// Level is a status's row level: a registry missing, unreadable, symlinked or
// mismatched fails; source and conflict states warn.
func (status AgentsStatus) Level() string {
	switch status.State {
	case AgentsLinked, AgentsInstalled, AgentsNoClone, AgentsNoClaude:
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
	statuses := Inspect(options)
	rows := make([]Row, 0, len(statuses))
	for _, status := range statuses {
		rows = append(rows, Row{Level: status.Level(), Message: "global-agents " + status.Describe()})
	}
	return rows
}

// Inspect classifies every registry against the source clone.
func Inspect(options DoctorOptions) []AgentsStatus {
	home := options.Home
	store := options.ClaudeConfigDir
	if store == "" {
		store = filepath.Join(home, ".claude")
	}
	if options.ClaudeAbsent {
		return []AgentsStatus{{Store: true, Dir: store, State: AgentsNoClaude}}
	}
	repo := options.SourceRepo
	if repo == "" {
		return []AgentsStatus{{Dir: filepath.Clean(filepath.Join(home, ".professor")), State: AgentsNoClone}}
	}
	agentsDir := filepath.Join(repo, "templates", "global", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return []AgentsStatus{{Dir: agentsDir, State: AgentsUnreadable, Error: err.Error()}}
	}
	var sources []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".md") {
			sources = append(sources, filepath.Join(agentsDir, entry.Name()))
		}
	}
	if len(sources) == 0 {
		return []AgentsStatus{{Dir: agentsDir, State: AgentsNoSources}}
	}
	sort.Strings(sources)
	variants, err := codexgen.LoadGlobalAgentVariants(agentsDir, paths.GeneratedClaudeAgentsDir(home))
	if err != nil {
		return []AgentsStatus{{Dir: agentsDir, State: AgentsUnreadable, Error: err.Error()}}
	}
	for _, variant := range variants {
		sources = append(sources, variant.Path)
	}
	statuses := []AgentsStatus{inspectStore(store, repo, sources)}
	roles, err := codexgen.CompileGlobalRoles(home, repo)
	if err != nil {
		return append(statuses, AgentsStatus{Dir: agentsDir, State: AgentsUnreadable, Error: err.Error()})
	}
	codexHomes := options.CodexHomes
	if len(codexHomes) == 0 {
		codexHomes = []string{filepath.Join(home, ".codex")}
	}
	for _, codexHome := range codexHomes {
		statuses = append(statuses, inspectCodexRegistry(filepath.Join(codexHome, "agents"), roles))
	}
	return statuses
}

func inspectCodexRegistry(registry string, roles []codexgen.GlobalRole) AgentsStatus {
	status := AgentsStatus{Dir: registry, State: AgentsInstalled}
	var symlinked, mismatched, missing, unreadable []string
	for _, role := range roles {
		target := filepath.Join(registry, role.Name+".toml")
		found, err := codexgen.ReadGlobalRoleFile(target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			missing = append(missing, role.Name)
		case errors.Is(err, codexgen.ErrGlobalRoleNotRegular) && isSymlink(target):
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
		name := strings.TrimSuffix(filepath.Base(source), ".md")
		state, _, err := codexgen.ClassifyGlobalLink(
			filepath.Join(registry, filepath.Base(source)), source, repo, codexgen.GlobalLinkFile,
		)
		if err != nil {
			status.State = AgentsUnreadable
			status.Error = err.Error()
			return status
		}
		switch state {
		case codexgen.GlobalLinkCorrect:
		case codexgen.GlobalLinkConflict:
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
