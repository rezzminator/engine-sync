package machine

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/engine"
	"github.com/rezzminator/engine-sync/internal/paths"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"github.com/rezzminator/engine-sync/internal/target/opencode"
	"os"
	"path/filepath"
	"strings"
)

// GlobalAgentsOptions selects the host HOME whose global Codex agents get
// (re)compiled and installed. The source directory is always
// {SourceRepo}/{machine.sourceAgentsDir}. Installs land at
// {ClaudeConfigDir}/agents for every configured Claude account (a symlink to
// the raw .md, Claude reads it directly) and at {CodexHome}/agents for every
// configured Codex home — there as a REGULAR FILE holding the compiled TOML,
// because Codex refuses to load a role through a symlink (globalrole.go).
type GlobalAgentsOptions struct {
	Source *source.Project
	Config *config.Config
	Home   string
	// SourceRepo selects machine.sourceAgentsDir inside a repository. Empty
	// uses the Claude config directory's agents registry as its source.
	SourceRepo string
	// ClaudeConfigDirs are the Claude config dirs whose agents/ registry
	// receives one link per global agent — every account the host has
	// configured, the same fanout the installer's retire paths already walk.
	// Nil or empty means {Home}/.claude alone: the legacy single-account
	// behavior every caller that never learned about accounts still gets.
	ClaudeConfigDirs []string
	// CodexHomes are the Codex homes whose agents/ registry receives one role
	// FILE per global agent — every account the host has configured. Nil or
	// empty means {Home}/.codex alone.
	CodexHomes []string
	Mode       Mode
}

// GlobalAgentCompiled is one desired role file or rendered variant source.
// Build writes changed bytes; check reports the same desired artifact without
// writing it.
type GlobalAgentCompiled struct {
	Path string
	Size int64
}

// GlobalAgentInstalled is one desired registry symlink and the shape check
// found there — the classification codex.GlobalLinkState names, so a
// missing, drifted, or foreign target can never certify itself as installed.
type GlobalAgentInstalled struct {
	Path   string
	Source string
	State  GlobalLinkState
	Found  string
}

// GlobalAgentsResult reports every desired artifact in the same two phases
// the host script performed. Actions narrows that roster to paths build would
// actually replace; Problems carries every conflict build refused to touch.
type GlobalAgentsResult struct {
	Source    *source.Project
	Compiled  []GlobalAgentCompiled
	Installed []GlobalAgentInstalled
	// Roles is one desired {CodexHome}/agents/<name>.toml per role per Codex
	// home, with the shape check found there before this run wrote anything.
	Roles    []GlobalRoleInstalled
	Actions  []GlobalAgentAction
	Warnings []string
	Problems []string
}

// GlobalRoleInstalled is one desired Codex role file and the shape check found
// at its path — the classification GlobalRoleState names, so a symlinked,
// drifted, or foreign role can never certify itself as installed. Found
// carries a symlink's resolved target and is empty otherwise.
type GlobalRoleInstalled = reconciler.GlobalRoleInstalled

// GlobalAgentAction is one exact path a build would replace. Check mode emits
// the same action list without touching disk, which lets the host installer
// present its complete plan before any earlier install mutation can land.
// Target is the symlink source for a "link" action; it is empty for the
// "write" actions the .toml compile step still performs.
type GlobalAgentAction struct {
	Kind   string
	Path   string
	Target string
}

// RunGlobalAgents compiles
// every {SourceRepo}/{machine.sourceAgentsDir}/*.md, validates every compiled
// TOML parses, then installs each agent twice.
//
// Claude gets a SYMLINK: every configured Claude agents registry
// (ClaudeConfigDirs, {Home}/.claude by default) points at the clone's .md, so
// an edit to the source propagates with no reinstall. A regular-file copy
// already at a desired target is replaced only with proven ownership. An
// unmarked copy from the old installer is refused, preserved and fails the
// run. An unowned symlink is also a conflict — see
// ClassifyGlobalLink in globallink.go.
//
// Codex gets a REGULAR FILE: {CodexHome}/agents/<name>.toml holds the compiled
// bytes outright, because Codex's role loader opens a role with O_NOFOLLOW and
// rejects a symlink as "agent type is currently not available". A role file
// engine-sync wrote is recognised by its generated marker and rewritten when it
// drifts; a pre-migration engine-sync symlink is replaced by the real file; anything
// else of that name is a conflict this never touches — see globalrole.go.
//
// TOML escaping mirrors build-codex.mjs:151-153 exactly — see
// globalAgentEscape / globalAgentEscapeMultiline — because a raw `"` in an
// agent description (real examples: the trigger phrases "walker fast",
// "map it now") breaks the TOML parser at Codex startup and takes the whole
// role down with it.
//
// A failed compile or an unparseable TOML aborts before any install, the
// same fail-loud order the host script used: an emitter that ships an
// unparseable artifact has done nothing useful.
func RunGlobalAgents(options GlobalAgentsOptions) (GlobalAgentsResult, error) {
	home, err := resolveMachineHome(options.Home)
	if err != nil {
		return GlobalAgentsResult{}, err
	}
	sourceRepo := strings.TrimSpace(options.SourceRepo)
	if sourceRepo != "" {
		sourceRepo, err = filepath.Abs(sourceRepo)
		if err != nil {
			return GlobalAgentsResult{}, err
		}
	}
	cfg := options.Config
	if cfg == nil {
		resolved, loadErr := config.LoadConfigLayers("", home)
		err = loadErr
		cfg = &resolved
	}
	if err != nil {
		return GlobalAgentsResult{}, err
	}
	sourceRoot, err := filepath.Abs(paths.GlobalSourceRoot(home, sourceRepo, cfg.ClaudeConfigDir))
	if err != nil {
		return GlobalAgentsResult{}, fmt.Errorf(
			"resolve global agents source repository %q: %w",
			options.SourceRepo,
			err,
		)
	}
	sourceRoot = filepath.Clean(sourceRoot)

	claudeConfigDirs := options.ClaudeConfigDirs
	if len(claudeConfigDirs) == 0 {
		claudeConfigDirs = []string{cfg.ClaudeConfigDir}
	}

	// Phase 1: compile and validate every role's TOML before touching disk —
	// a failed compile or an unparseable TOML aborts before any install, the
	// same fail-loud order the host script used.
	model := options.Source
	if model == nil {
		loaded, e := source.LoadMachine(home, source.AgentsDirectory(home, sourceRepo, *cfg), source.GeneratedDirectory(home, sourceRepo, *cfg), source.MachineOptions{MarkerDir: cfg.Machine.AgentMarkerDir, ClaudeConfigDirs: claudeConfigDirs})
		if e != nil {
			return GlobalAgentsResult{}, e
		}
		model = &loaded
	}
	unfiltered := *model
	filtered, skipped := filterMachineAgents(*model, cfg.Capabilities)
	filtered = skipBrokenAgents(filtered, cfg)
	unfiltered.Problems, unfiltered.Failed = filtered.Problems, filtered.Failed
	model = &filtered
	compiledAgents, err := compileMachineAgents(*model, cfg)
	if err != nil {
		return GlobalAgentsResult{}, err
	}

	// One Claude agents/ registry per configured account; an installer that
	// never names its accounts keeps the single {Home}/.claude registry.

	// One Codex agents/ registry per configured Codex home; a caller that
	// never names its accounts (a nil roster, `engine-sync codex agents`) keeps the
	// single {Home}/.codex registry, while a non-nil empty roster means no
	// Codex home at all: no role is planned or written.
	codexHomes := options.CodexHomes
	if codexHomes == nil {
		codexHomes = []string{filepath.Join(home, ".codex")}
	}
	// The registries a pre-migration role symlink is allowed to point into —
	// the retired generated store, and the even older in-clone twin. A link
	// resolving anywhere else was never written by engine-sync.
	ownedLinkDirs := []string{
		filepath.Join(home, cfg.Machine.LegacyCodexAgentsDir),
		source.AgentsDirectory(home, sourceRepo, *cfg),
	}

	type desiredLink struct {
		target string
		source string
	}
	type desiredRole struct {
		target  string
		content []byte
	}
	// Preserve declarations for the orphan sweep and the next build pass.
	result := GlobalAgentsResult{Source: &unfiltered}
	result.Problems = append(result.Problems, unfiltered.Problems...)
	links := make([]desiredLink, 0, len(compiledAgents)*len(claudeConfigDirs))
	roles := make([]desiredRole, 0, len(compiledAgents)*len(codexHomes))
	for _, agent := range compiledAgents {
		for _, codexHome := range codexHomes {
			if agent.tomlContent == nil {
				break
			}
			target := filepath.Join(codexHome, "agents", agent.name+".toml")
			roles = append(roles, desiredRole{target: target, content: agent.tomlContent})
			result.Compiled = append(
				result.Compiled,
				GlobalAgentCompiled{Path: target, Size: int64(len(agent.tomlContent))},
			)
		}
		if agent.mdContent != nil {
			result.Compiled = append(
				result.Compiled,
				GlobalAgentCompiled{Path: agent.mdSource, Size: int64(len(agent.mdContent))},
			)
			same, err := sameGlobalAgentFile(agent.mdSource, agent.mdContent)
			if err != nil {
				return GlobalAgentsResult{}, err
			}
			if !same {
				result.Actions = append(result.Actions, GlobalAgentAction{Kind: actionWrite, Path: agent.mdSource})
			}
		}
		for _, config := range claudeConfigDirs {
			same, e := paths.SameEntry(filepath.Join(config, "agents", filepath.Base(agent.mdSource)), agent.mdSource)
			if e != nil {
				return GlobalAgentsResult{}, e
			}
			if same {
				continue
			}
			links = append(
				links,
				desiredLink{
					target: filepath.Join(config, "agents", filepath.Base(agent.mdSource)),
					source: agent.mdSource,
				},
			)
		}
	}

	// Phase 2a: classify every desired role file against what is actually on
	// disk. A foreign file is a Problem and never an Action — it stays exactly
	// as the operator left it.
	for _, role := range roles {
		state, foundAt, err := ClassifyGlobalRole(role.target, role.content, ownedLinkDirs)
		if err != nil {
			return GlobalAgentsResult{}, fmt.Errorf("inspect global role %s: %w", role.target, err)
		}
		found := GlobalRoleInstalled{Path: role.target, State: state, Found: foundAt}
		result.Roles = append(result.Roles, found)
		switch state {
		case GlobalRoleForeign:
			// Reconciliation below reports the conflict once.
			continue
		case GlobalRoleCurrent:
			continue
		}
		result.Actions = append(result.Actions, GlobalAgentAction{Kind: actionWrite, Path: role.target})
	}

	// Phase 2b: classify every desired registry symlink against what is
	// actually on disk. A stat failure other than "not found" bubbles up as
	// a genuine error — an unreadable target is never reported as absent.
	for _, link := range links {
		state, found, err := ClassifyGlobalLink(link.target, link.source, sourceRoot, GlobalLinkFile)
		if err != nil {
			return GlobalAgentsResult{}, fmt.Errorf("inspect global agent artifact %s: %w", link.target, err)
		}
		result.Installed = append(
			result.Installed,
			GlobalAgentInstalled{Path: link.target, Source: link.source, State: state, Found: found},
		)
		switch state {
		case GlobalLinkConflict:
			result.Problems = append(result.Problems, DescribeGlobalLinkState(state, link.target, link.source, found))
			continue
		case GlobalLinkCorrect:
			continue
		}
		result.Actions = append(
			result.Actions,
			GlobalAgentAction{Kind: actionLink, Path: link.target, Target: link.source},
		)
	}

	model.Root = sourceRoot
	model.CodexHomes = codexHomes
	model.ClaudeConfigDirs = claudeConfigDirs
	target := codex.Target{}
	mode := reconciler.Build
	if options.Mode == ModeCheck {
		mode = reconciler.Plan
	}

	cleanup := run{options: Options{Home: home, SourceRepo: sourceRepo, ClaudeConfigDirs: claudeConfigDirs, CodexHomes: codexHomes, Plan: options.Mode == ModeCheck}, policy: *cfg}
	if err := cleanup.retireExcludedMachineAgents(unfiltered); err != nil {
		return GlobalAgentsResult{}, fmt.Errorf("retire excluded Claude agents: %w", err)
	}
	// The source problems are reported once above; a skipped agent never
	// stops the healthy ones from building.
	model.Problems = nil
	applied, _ := engine.Execute(*model, *cfg, mode, target.Render, target.Roots, target.Validate)
	openCodeTarget := opencode.Target{}
	openCodeApplied, _ := engine.Execute(*model, *cfg, mode, openCodeTarget.Render, openCodeTarget.Roots, openCodeTarget.Validate)
	applied.Warnings = append(applied.Warnings, openCodeApplied.Warnings...)
	applied.Actions = append(applied.Actions, openCodeApplied.Actions...)
	applied.Rebuildable = append(applied.Rebuildable, openCodeApplied.Rebuildable...)
	applied.Problems = append(applied.Problems, openCodeApplied.Problems...)
	result.Warnings = append(skipped, applied.Warnings...)
	result.Actions = nil
	for _, event := range cleanup.report.Events {
		if event.Verb == VerbRetire {
			result.Actions = append(result.Actions, GlobalAgentAction{Kind: "delete", Path: event.Path})
		}
		if event.Verb == VerbSkip {
			result.Warnings = append(result.Warnings, event.Message)
		}
	}
	for _, a := range applied.Actions {
		result.Actions = append(result.Actions, GlobalAgentAction{Kind: a.Kind, Path: a.Path, Target: a.Target})
	}
	rebuildable := map[string]bool{}
	for _, p := range applied.Rebuildable {
		rebuildable[p] = true
	}
	for _, problem := range applied.Problems {
		duplicate := false
		for _, link := range result.Installed {
			if link.State == GlobalLinkConflict && strings.Contains(problem, link.Path) {
				duplicate = true
			}
		}
		if !rebuildable[problem] && !duplicate {
			result.Problems = append(result.Problems, problem)
		}
	}

	return result, nil
}

// compiledAgent is one global agent rendered for both engines. mdContent is
// nil for an original agent (Claude links straight to the clone's .md) and
// carries the rendered bytes for a variant, whose mdSource is its file in the
// generated Claude directory: a variant is the Claude -> Claude step, and from
// there it takes the same link and the same role file as any original.
type compiledAgent struct {
	name        string
	mdSource    string
	mdContent   []byte
	tomlContent []byte
}

// compileGlobalAgents renders every machine-global agent — originals plus the
// rendered declarations — that global installs: an agent whose
// requires is unmet or that skipBrokenAgents skips is left out, as global
// leaves it out, and the caller names those (InspectRegistries' BROKEN row).
func compileGlobalAgents(home, sourceRepo string, cfg *config.Config, claudeConfigDirs []string) ([]compiledAgent, error) {
	agentsDir := source.AgentsDirectory(home, sourceRepo, *cfg)
	if len(claudeConfigDirs) == 0 {
		claudeConfigDirs = []string{cfg.ClaudeConfigDir}
	}
	model, err := source.LoadMachine(home, agentsDir, source.GeneratedDirectory(home, sourceRepo, *cfg), source.MachineOptions{MarkerDir: cfg.Machine.AgentMarkerDir, ClaudeConfigDirs: claudeConfigDirs})
	if err != nil {
		return nil, err
	}
	filtered, _ := filterMachineAgents(model, cfg.Capabilities)
	return compileMachineAgents(skipBrokenAgents(filtered, cfg), cfg)
}
func compileMachineAgents(model source.Project, cfg *config.Config) ([]compiledAgent, error) {
	agentsDir := model.AgentSourceDir
	agentSources := model.Agents
	compiled := make([]compiledAgent, 0, len(agentSources))
	for _, src := range agentSources {
		raw := src.Raw
		name, content, err := codex.RenderMachineAgent(src.Path, string(raw), agentsDir, *cfg)
		if err != nil {
			return nil, err
		}
		if parseErr := codex.ValidateRole(content); parseErr != nil {
			return nil, fmt.Errorf("%s: does not parse: %w", src.Path, parseErr)
		}
		// engines.codex: false — Claude still links the agent; no role
		// file is desired, and the reconcile orphan walk retires an owned one.
		var toml []byte
		if !src.Front.Engines.CodexOff() {
			toml = []byte(content)
		}
		compiled = append(compiled, compiledAgent{
			name:     name,
			mdSource: src.Path,
			mdContent: func() []byte {
				if src.Generated {
					return src.Raw
				}
				return nil
			}(),
			tomlContent: toml,
		})
	}
	return compiled, nil
}

// GlobalRole is one machine-global role as THIS binary compiles it: the name
// its registry file takes and the exact bytes that file must hold. The doctor
// reads it to compare disk against the compiler that owns it — a role file is
// correct only when it matches what a reinstall would write, never merely
// because something exists at the path.
type GlobalRole struct {
	Name    string
	Content []byte
}

// CompileGlobalRoles is the read-only half of RunGlobalAgents' compile phase,
// for callers that need the desired bytes without installing anything.
// claudeConfigDirs is the Claude roster global installs into, so a source
// directory inside any of them is the same named collision global reports;
// empty means the policy's one config dir.
func CompileGlobalRoles(home, sourceRepo string, claudeConfigDirs []string, policies ...*config.Config) ([]GlobalRole, error) {
	resolvedHome, err := resolveMachineHome(home)
	if err != nil {
		return nil, err
	}
	var cfg *config.Config
	if len(policies) > 0 {
		cfg = policies[0]
	}
	if cfg == nil {
		resolved, err := config.LoadConfigLayers("", resolvedHome)
		if err != nil {
			return nil, err
		}
		cfg = &resolved
	}
	compiled, err := compileGlobalAgents(resolvedHome, sourceRepo, cfg, claudeConfigDirs)
	if err != nil {
		return nil, err
	}
	roles := make([]GlobalRole, 0, len(compiled))
	for _, agent := range compiled {
		if agent.tomlContent == nil {
			continue
		}
		roles = append(roles, GlobalRole{Name: agent.name, Content: agent.tomlContent})
	}
	return roles, nil
}

func sameGlobalAgentFile(path string, content []byte) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect global agent artifact %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read global agent artifact %s: %w", path, err)
	}
	return string(raw) == string(content), nil
}

// filterMachineAgents keeps the agents whose requires the host's
// capabilities meet and names each other one in a SKIPPED line.
func filterMachineAgents(model source.Project, capabilities []string) (source.Project, []string) {
	agents := make([]source.Agent, 0, len(model.Agents))
	var skipped []string
	for _, agent := range model.Agents {
		if unmet := source.Unmet(agent.Front.Requires, capabilities); unmet != "" {
			skipped = append(skipped, source.Skipped(agentName(agent), unmet))
			if agent.Generated {
				model.Excluded = append(model.Excluded, agent)
			}
			continue
		}
		agents = append(agents, agent)
	}
	model.Agents = agents
	return model, skipped
}

// agentName is the agent's frontmatter name, else its file name.
func agentName(agent source.Agent) string {
	if agent.Name != "" {
		return agent.Name
	}
	return strings.TrimSuffix(filepath.Base(agent.Path), ".md")
}
