package machine

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const declaredAgentSource = "---\nname: lead\ndescription: >\n  Folded lead description\n  over two lines.\ntools: Read, Agent\n" +
	"model: opus # tier note\neffort: low\n---\n\nLead body, byte for byte.\n\n## Section\n\n\"quoted\" text.\n"

func declarationsDir(home string) string {
	return filepath.Join(home, "source-repo", "agents")
}

func TestAbsentDeclarationsRenderNothing(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(declarationsDir(home), "lead.md"), declaredAgentSource)
	agents, err := source.LoadDeclarations(declarationsDir(home), filepath.Join(home, "out"))
	if err != nil || agents != nil {
		t.Fatalf("agents = %#v, err = %v; want nil, nil", agents, err)
	}
}

// A malformed source declaration is named while healthy originals compile.
func TestGlobalAgentDeclarationFailureKeepsHealthyAgents(t *testing.T) {
	home := testjail.Scratch(t)
	dir := declarationsDir(home)
	writeAgentFixture(t, filepath.Join(dir, "lead.md"), declaredAgentSource)
	writeAgentFixture(t, filepath.Join(dir, "super-lead.md"), "---\nname: super-lead\nfrom: [lead]\n---\n")
	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil || len(result.Problems) != 1 || !strings.Contains(result.Problems[0], "DECLARATION-INVALID") || len(result.Compiled) == 0 {
		t.Fatalf("result = %+v (%v)", result, err)
	}
}

func TestGlobalAgentsRenderLinkAndCompileAGeneratedAgent(t *testing.T) {
	home := testjail.Scratch(t)
	dir := declarationsDir(home)
	second := filepath.Join(home, ".cc", "2")
	writeAgentFixture(t, filepath.Join(dir, "lead.md"), declaredAgentSource)
	writeAgentFixture(t, filepath.Join(dir, "super-lead.md"), "---\nname: super-lead\nfrom: lead\ndescription: Deep lead.\neffort: medium\n---\n")
	options := GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home, ClaudeConfigDirs: []string{filepath.Join(home, ".claude"), second}}
	generatedMD := filepath.Join(home, "source-repo", "generated", "agents", "super-lead.md")

	options.Mode = ModeCheck
	plan, err := runFixtureMachineAgents(options)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if _, err := os.Lstat(generatedMD); !os.IsNotExist(err) {
		t.Fatalf("check mode wrote %s (err=%v)", generatedMD, err)
	}
	wroteMD := false
	for _, action := range plan.Actions {
		if action.Kind == actionWrite && action.Path == generatedMD {
			wroteMD = true
		}
	}
	if !wroteMD {
		t.Fatalf("check plan never names the agent write: %#v", plan.Actions)
	}
	for _, config := range options.ClaudeConfigDirs {
		if !hasGlobalAgentLinkAction(plan.Actions, filepath.Join(config, "agents", "super-lead.md"), generatedMD) {
			t.Fatalf("check plan never links the agent into %s: %#v", config, plan.Actions)
		}
	}

	options.Mode = ModeBuild
	if _, err := runFixtureMachineAgents(options); err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, config := range options.ClaudeConfigDirs {
		assertGlobalSymlink(t, filepath.Join(config, "agents", "super-lead.md"), generatedMD)
		assertGlobalSymlink(t, filepath.Join(config, "agents", "lead.md"), filepath.Join(dir, "lead.md"))
	}
	toml := filepath.Join(home, ".codex", "agents", "super-lead.toml")
	assertGlobalRoleFile(t, toml)
	content := string(readAgentFixture(t, toml))
	if !strings.Contains(content, `name = "super-lead"`) || !strings.Contains(content, `description = "Deep lead."`) ||
		!strings.Contains(content, "Lead body, byte for byte.") {
		t.Fatalf("agent TOML twin:\n%s", content)
	}

	options.Mode = ModeCheck
	settled, err := runFixtureMachineAgents(options)
	if err != nil {
		t.Fatalf("re-check: %v", err)
	}
	if len(settled.Actions) != 0 || len(settled.Problems) != 0 {
		t.Fatalf("a built host still plans work: actions=%#v problems=%#v", settled.Actions, settled.Problems)
	}
}

// Source-file fixtures render through the real compiler, pins included.
func TestShippedDeclarationsRender(t *testing.T) {
	agentsDir := filepath.Join("testdata", "global-declarations")
	declared := []string{"digger-pro", "digger-max"}
	agents, err := source.LoadDeclarations(agentsDir, testjail.Scratch(t))
	if err != nil {
		t.Fatalf("agents fixture: %v", err)
	}
	if len(agents) != len(declared) {
		t.Fatalf("rendered %d agents, the fixture declares %d", len(agents), len(declared))
	}
	byName := map[string]string{}
	for _, agent := range agents {
		role, toml, err := renderFixtureMachineAgent(agent.Path, string(agent.Content), testjail.Scratch(t))
		if err != nil {
			t.Fatalf("agent %s does not compile to TOML: %v", agent.Name, err)
		}
		if role != agent.Name || !strings.Contains(toml, `name = "`+agent.Name+`"`) {
			t.Fatalf("agent %s compiled to role %q:\n%s", agent.Name, role, toml)
		}
		byName[agent.Name] = string(agent.Content)
	}
	if got := byName["digger-pro"]; !strings.Contains(got, "Spawn at most 6 helpers a round") ||
		strings.Contains(got, "Spawn at most 4 helpers a round") || !strings.Contains(got, "effort: medium") {
		t.Fatalf("digger-pro did not apply its swaps and override:\n%s", got)
	}
	if got := byName["digger-max"]; !strings.Contains(got, "`surveyor` digs a repository") ||
		!strings.Contains(got, "tools: WebSearch, WebFetch, Write, Agent\n") {
		t.Fatalf("digger-max did not apply its swap and override:\n%s", got)
	}
}

// TestGlobalAgentsRenderIntoTheSourceRepository: with
// machine.sourceGeneratedAgentsDir set, a declaration renders into that
// directory of the source repository, every Claude registry links there, and
// a second run plans nothing.
func TestGlobalAgentsRenderIntoTheSourceRepository(t *testing.T) {
	home := testjail.Scratch(t)
	dir := declarationsDir(home)
	writeAgentFixture(t, filepath.Join(dir, "lead.md"), declaredAgentSource)
	writeAgentFixture(t, filepath.Join(dir, "super-lead.md"), "---\nname: super-lead\nfrom: lead\ndescription: Deep lead.\neffort: medium\n---\n")
	cfg, err := config.LoadConfigLayers("", home)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Codex.ModelMap = fixtureMachinePolicy().Codex.ModelMap
	cfg.Codex.Review = fixtureMachinePolicy().Codex.Review
	cfg.Machine.SourceGeneratedAgentsDir = "generated/agents"
	repo := filepath.Join(home, "source-repo")
	options := GlobalAgentsOptions{SourceRepo: repo, Home: home, Config: &cfg, Mode: ModeBuild}
	if _, err := RunGlobalAgents(options); err != nil {
		t.Fatalf("build: %v", err)
	}
	rendered := filepath.Join(repo, "generated", "agents", "super-lead.md")
	assertGlobalSymlink(t, filepath.Join(home, ".claude", "agents", "super-lead.md"), rendered)
	if !strings.Contains(string(readAgentFixture(t, rendered)), "Lead body, byte for byte.") {
		t.Fatalf("%s lacks the base body", rendered)
	}
	options.Mode = ModeCheck
	settled, err := RunGlobalAgents(options)
	if err != nil || len(settled.Actions) != 0 || len(settled.Problems) != 0 {
		t.Fatalf("re-check: actions=%#v problems=%#v (%v)", settled.Actions, settled.Problems, err)
	}
	// A declaration the host's capabilities exclude keeps its repository
	// render, without a link.
	writeAgentFixture(t, filepath.Join(dir, "gated-lead.md"), "---\nname: gated-lead\nfrom: lead\ndescription: Gated lead.\nrequires: [gated]\n---\n")
	options.Mode = ModeBuild
	if _, err := RunGlobalAgents(options); err != nil {
		t.Fatalf("build with an excluded declaration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "generated", "agents", "gated-lead.md")); err != nil {
		t.Fatalf("excluded declaration's repository render missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "agents", "gated-lead.md")); !os.IsNotExist(err) {
		t.Fatalf("excluded declaration linked (err=%v)", err)
	}
}

// TestDoctorNamesAGeneratedOrphan: a rendered file in the generated directory
// whose declaration is gone is an ORPHAN row that fails doctor; a declared
// one is not named.
func TestDoctorNamesAGeneratedOrphan(t *testing.T) {
	home := testjail.Scratch(t)
	dir := declarationsDir(home)
	writeAgentFixture(t, filepath.Join(dir, "lead.md"), declaredAgentSource)
	writeAgentFixture(t, filepath.Join(dir, "super-lead.md"), "---\nname: super-lead\nfrom: lead\ndescription: Deep lead.\n---\n")
	cfg, err := config.LoadConfigLayers("", home)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Machine.SourceGeneratedAgentsDir = "generated/agents"
	repo := filepath.Join(home, "source-repo")
	generated := filepath.Join(repo, "generated", "agents")
	writeAgentFixture(t, filepath.Join(generated, "super-lead.md"), "---\nname: super-lead\n---\n")
	writeAgentFixture(t, filepath.Join(generated, "ghost.md"), "---\nname: ghost\n---\n")
	var orphan *AgentsStatus
	for _, status := range InspectRegistries(DoctorOptions{Config: &cfg, Home: home, SourceRepo: repo}) {
		if status.State == AgentsOrphan {
			orphan = &status
		}
	}
	if orphan == nil || orphan.Level() != LevelFail || len(orphan.Names) != 1 || filepath.Base(orphan.Names[0]) != "ghost.md" {
		t.Fatalf("orphan row = %#v, want one failing ORPHAN naming ghost.md", orphan)
	}
}
