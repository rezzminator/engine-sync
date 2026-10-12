package machine

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/paths"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target/opencode"
)

// The closed-world states of one declared machine-global agent in one engine
// registry, plus STRAY for an unowned file at an undeclared name.
const (
	StateInstalled = "INSTALLED"
	StateMissing   = string(reconciler.Missing)
	StateStale     = string(reconciler.Stale)
	StateShadowed  = "SHADOWED"
	StateSkipped   = "SKIPPED"
	StateStray     = "STRAY"
)

// The engines a machine-global agent reaches.
const (
	EngineClaude   = "claude"
	EngineCodex    = "codex"
	EngineOpenCode = "opencode"
)

// AgentState is one declared agent's state at one path, or one STRAY file in
// a managed registry. Detail carries what was found when it explains the
// state.
type AgentState struct {
	Engine string `json:"engine"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// Level is the doctor row level of a state: INSTALLED and SKIPPED pass, STRAY
// warns, and every state a run of `engine-sync global` would change fails.
func (s AgentState) Level() string {
	switch s.State {
	case StateInstalled, StateSkipped:
		return LevelOK
	case StateStray:
		return LevelWarn
	}
	return LevelFail
}

// Describe is the one operator-facing line of a state.
func (s AgentState) Describe() string {
	line := fmt.Sprintf("agent %s %s %s %s", s.State, s.Engine, s.Name, s.Path)
	if s.Detail != "" {
		line += " (" + s.Detail + ")"
	}
	switch s.State {
	case StateMissing, StateStale:
		line += ` hint="run engine-sync global"`
	case StateShadowed:
		line += ` hint="move the file aside, or run engine-sync global --adopt"`
	}
	return line
}

// errRenderFailures marks an AgentStates result whose agents loaded but some
// failed to render: the states listed are real, the declared set incomplete.
var errRenderFailures = errors.New("load or render agents")

// AgentStates classifies every declared machine-global agent, original and
// variant, closed-world per engine: one state per Claude registry link, per
// Codex role file and per OpenCode agent file, then every STRAY in the Codex
// and OpenCode registries. The Claude registry is shared with hand-installed
// agents, so it reports no STRAY. An owned file at an undeclared name is
// STALE: `engine-sync global` retires it.
func AgentStates(home, sourceRepo string, cfg *config.Config, claudeConfigDirs, codexHomes []string) ([]AgentState, error) {
	if sourceRepo != "" {
		abs, err := filepath.Abs(sourceRepo)
		if err != nil {
			return nil, fmt.Errorf("resolve source repository %q: %w", sourceRepo, err)
		}
		sourceRepo = abs
	}
	if len(claudeConfigDirs) == 0 {
		claudeConfigDirs = []string{cfg.ClaudeConfigDir}
	}
	agentsDir := source.AgentsDirectory(home, sourceRepo, *cfg)
	model, err := source.LoadMachine(home, agentsDir, filepath.Join(home, cfg.Machine.GeneratedAgentsDir), source.MachineOptions{MarkerDir: cfg.Machine.AgentMarkerDir, ClaudeConfigDirs: claudeConfigDirs})
	if err != nil {
		return nil, fmt.Errorf("load machine agents from %s: %w", agentsDir, err)
	}
	sourceRoot, err := filepath.Abs(paths.GlobalSourceRoot(home, sourceRepo, cfg.ClaudeConfigDir))
	if err != nil {
		return nil, fmt.Errorf("resolve global agents source repository %q: %w", sourceRepo, err)
	}
	var states []AgentState
	for _, agent := range model.Agents {
		if missing := source.Unmet(agent.Front.Requires, cfg.Capabilities); missing != "" {
			states = append(states, AgentState{Engine: "all", Name: agentName(agent), Path: agent.Path, State: StateSkipped, Detail: "requires " + missing + " unmet"})
		}
	}
	filtered, _ := filterMachineAgents(model, cfg.Capabilities)
	filtered = skipBrokenAgents(filtered, cfg)
	compiled, err := compileMachineAgents(filtered, cfg)
	if err != nil {
		return nil, err
	}
	if codexHomes == nil {
		codexHomes = []string{filepath.Join(home, ".codex")}
	}
	ownedLinkDirs := []string{filepath.Join(home, cfg.Machine.LegacyCodexAgentsDir), agentsDir}
	declaredRoles := map[string]bool{}
	for _, name := range filtered.Failed {
		for _, codexHome := range codexHomes {
			declaredRoles[filepath.Join(codexHome, "agents", name+".toml")] = true // kept, never STALE
		}
	}
	for _, agent := range compiled {
		for _, dir := range claudeConfigDirs {
			path := filepath.Join(dir, "agents", filepath.Base(agent.mdSource))
			if same, err := paths.SameEntry(path, agent.mdSource); err != nil {
				return nil, fmt.Errorf("inspect global agent link %s: %w", path, err)
			} else if same {
				// The registry is the source directory itself.
				states = append(states, AgentState{Engine: EngineClaude, Name: agent.name, Path: path, State: StateInstalled})
				continue
			}
			state, found, err := ClassifyGlobalLink(path, agent.mdSource, filepath.Clean(sourceRoot), GlobalLinkFile)
			if err != nil {
				return nil, fmt.Errorf("inspect global agent link %s: %w", path, err)
			}
			s := AgentState{Engine: EngineClaude, Name: agent.name, Path: path, Detail: found}
			switch state {
			case GlobalLinkCorrect:
				s.State, s.Detail = StateInstalled, ""
				if agent.mdContent != nil {
					same, err := sameGlobalAgentFile(agent.mdSource, agent.mdContent)
					if err != nil {
						return nil, err
					}
					if !same {
						s.State, s.Detail = StateStale, "rendered variant "+agent.mdSource+" is not current"
					}
				}
			case GlobalLinkMissing:
				s.State = StateMissing
			case GlobalLinkConflict:
				s.State = StateShadowed
			default:
				s.State = StateStale
			}
			states = append(states, s)
		}
		if agent.tomlContent == nil {
			continue
		}
		for _, codexHome := range codexHomes {
			path := filepath.Join(codexHome, "agents", agent.name+".toml")
			declaredRoles[path] = true
			state, found, err := ClassifyGlobalRole(path, agent.tomlContent, ownedLinkDirs)
			if err != nil {
				return nil, fmt.Errorf("inspect global role %s: %w", path, err)
			}
			s := AgentState{Engine: EngineCodex, Name: agent.name, Path: path, Detail: found}
			switch state {
			case GlobalRoleCurrent:
				s.State = StateInstalled
			case GlobalRoleMissing:
				s.State = StateMissing
			case GlobalRoleForeign:
				s.State = StateShadowed
			default:
				s.State = StateStale
			}
			states = append(states, s)
		}
	}
	for _, codexHome := range codexHomes {
		strays, err := registryStrays(EngineCodex, filepath.Join(codexHome, "agents"), ".toml", declaredRoles, ownedLinkDirs)
		if err != nil {
			return nil, err
		}
		states = append(states, strays...)
	}
	filtered.Home = home
	rendered, diagnostics := opencode.Target{}.Render(filtered, *cfg)
	renderFailures := append([]string(nil), filtered.Problems...)
	for _, d := range diagnostics {
		if d.Severity == diag.Error {
			renderFailures = append(renderFailures, d.Message)
		}
	}
	declaredOpenCode := map[string]bool{}
	for _, a := range rendered {
		declaredOpenCode[a.Path] = true
		if a.Kept != nil {
			continue // an agent skipped for an error: its copy is kept as it is
		}
		state, err := classifyOwnedFile(a.Path, a.Content)
		if err != nil {
			return nil, err
		}
		states = append(states, AgentState{Engine: EngineOpenCode, Name: strings.TrimSuffix(filepath.Base(a.Path), ".md"), Path: a.Path, State: state})
	}
	if len(renderFailures) > 0 {
		// The declared set is unknown, so no owned file is called STALE.
		return states, fmt.Errorf("%w: %s", errRenderFailures, strings.Join(renderFailures, "; "))
	}
	strays, err := registryStrays(EngineOpenCode, opencode.MachineAgentsDir(home), ".md", declaredOpenCode, nil)
	if err != nil {
		return nil, err
	}
	return append(states, strays...), nil
}

// classifyOwnedFile is one generated regular file's state: absent is
// MISSING, the exact bytes are INSTALLED, any other marker-owned file is
// STALE, and anything else at the path is SHADOWED.
func classifyOwnedFile(path string, want []byte) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return StateMissing, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return StateShadowed, nil
	}
	found, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	switch {
	case bytes.Equal(found, want):
		return StateInstalled, nil
	case artifact.Claims(string(found)):
		return StateStale, nil
	}
	return StateShadowed, nil
}

// registryStrays walks one managed registry for files at undeclared names:
// an owned one (a marker-carrying file, or a link into ownedLinkDirs) is
// STALE, anything else is a STRAY warning. An absent registry has none; an
// unreadable one is an error, never an empty registry.
func registryStrays(engine, registry, suffix string, declared map[string]bool, ownedLinkDirs []string) ([]AgentState, error) {
	entries, err := os.ReadDir(registry)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect %s registry %s: %w", engine, registry, err)
	}
	var states []AgentState
	for _, entry := range entries {
		path := filepath.Join(registry, entry.Name())
		if !strings.HasSuffix(entry.Name(), suffix) || declared[path] {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", path, err)
		}
		owned := false
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return nil, fmt.Errorf("read link %s: %w", path, err)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(registry, target)
			}
			for _, dir := range ownedLinkDirs {
				owned = owned || within(filepath.Clean(target), dir)
			}
		case info.Mode().IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", path, err)
			}
			owned = artifact.Claims(string(content))
		}
		s := AgentState{Engine: engine, Name: strings.TrimSuffix(entry.Name(), suffix), Path: path, State: StateStray}
		if owned {
			s.State, s.Detail = StateStale, "owned, no longer declared"
		}
		states = append(states, s)
	}
	sort.SliceStable(states, func(i, j int) bool { return states[i].Path < states[j].Path })
	return states, nil
}

// CheckSourceVariants validates the machine-global agents a project carries
// in {root}/{machine.sourceAgentsDir}, its declarations included: every
// source and every rendered agent must parse,
// validate and compile, and each agent whose requires the capabilities do
// not meet is a SKIPPED warning. Each agent global would skip as broken is a
// problem with global's message: both run skipBrokenAgents. A project without
// any declarations has nothing to render. A failed render is a problem,
// never an absent agent.
func CheckSourceVariants(root, home string, cfg config.Config) (problems, warnings []string) {
	// Resolve the project root before joining its configured agents directory.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return []string{fmt.Sprintf("resolve project root %q: %v", root, err)}, nil
	}
	agentsDir := filepath.Join(absRoot, filepath.FromSlash(cfg.Machine.SourceAgentsDir))
	model, err := source.LoadMachine(home, agentsDir, filepath.Join(home, cfg.Machine.GeneratedAgentsDir), source.MachineOptions{MarkerDir: cfg.Machine.AgentMarkerDir, ClaudeConfigDirs: []string{cfg.ClaudeConfigDir}})
	if err != nil {
		return []string{err.Error()}, nil
	}
	problems = append(problems, model.Problems...)
	for _, agent := range model.Agents {
		p, w := source.ValidateAgent(agent.Path, agent.Front, cfg.Agents.PassthroughKeys, agent.Generated)
		problems = append(problems, p...)
		warnings = append(warnings, w...)
	}
	filtered, skipped := filterMachineAgents(model, cfg.Capabilities)
	warnings = append(warnings, skipped...)
	checked := skipBrokenAgents(filtered, &cfg)
	problems = append(problems, checked.Problems[len(filtered.Problems):]...)
	return problems, warnings
}

// retireOwnedEntries retires every engine-sync-owned entry of a directory the
// user shares, leaving each unowned entry and the directory itself in place.
func (r *run) retireOwnedEntries(dir, reason string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		owned, err := reconciler.Claimable(path, nil)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if !owned {
			continue
		}
		if err := r.retire(path, reason); err != nil {
			return err
		}
	}
	return nil
}
