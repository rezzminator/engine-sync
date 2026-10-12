package machine

import (
	"encoding/json"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const variantSourceAgent = "---\nname: lead\ndescription: >\n  Folded lead description\n  over two lines.\ntools: Read, Agent\n" +
	"model: opus # tier note\neffort: low\n---\n\nLead body, byte for byte.\n\n## Section\n\n\"quoted\" text.\n"

func variantAgentsDir(home string) string {
	return filepath.Join(home, "source-repo", "agents")
}

func TestGlobalAgentVariantsAbsentDeclarationIsNoVariants(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(variantAgentsDir(home), "lead.md"), variantSourceAgent)
	variants, err := source.LoadGlobalAgentVariants(variantAgentsDir(home), filepath.Join(home, "out"))
	if err != nil || variants != nil {
		t.Fatalf("variants = %#v, err = %v; want nil, nil", variants, err)
	}
}

// The machine path refuses what the loader refuses: a retired version 1
// declaration fails the run as one named problem, naming the rewrite, while
// the original still compiles (every other declaration error is
// TestVariantsV2Render's, in internal/source).
func TestGlobalAgentVariantsRetiredDeclarationFailsTheRun(t *testing.T) {
	home := testjail.Scratch(t)
	dir := variantAgentsDir(home)
	writeAgentFixture(t, filepath.Join(dir, "lead.md"), variantSourceAgent)
	writeAgentFixture(t, filepath.Join(dir, source.GlobalAgentVariantsFile), `{"super-lead":{"from":"lead","effort":"medium"}}`)
	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil || len(result.Problems) != 1 || !strings.Contains(result.Problems[0], "VARIANTS-INVALID ") || !strings.Contains(result.Problems[0], "version 1 is retired") || len(result.Compiled) == 0 {
		t.Fatalf("result = %+v, err = %v; want the version 1 retirement as the one problem", result, err)
	}
}

func TestGlobalAgentsRenderLinkAndCompileAVariant(t *testing.T) {
	home := testjail.Scratch(t)
	dir := variantAgentsDir(home)
	second := filepath.Join(home, ".cc", "2")
	writeAgentFixture(t, filepath.Join(dir, "lead.md"), variantSourceAgent)
	writeAgentFixture(t, filepath.Join(dir, source.GlobalAgentVariantsFile),
		`{"version":2,"variants":{"super-lead":{"from":"lead","set":{"description":"Deep lead.","effort":"medium"}}}}`)
	options := GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home, ClaudeConfigDirs: []string{filepath.Join(home, ".claude"), second}}
	generatedMD := filepath.Join(paths.GeneratedClaudeAgentsDir(home), "super-lead.md")

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
		t.Fatalf("check plan never names the variant write: %#v", plan.Actions)
	}
	for _, config := range options.ClaudeConfigDirs {
		if !hasGlobalAgentLinkAction(plan.Actions, filepath.Join(config, "agents", "super-lead.md"), generatedMD) {
			t.Fatalf("check plan never links the variant into %s: %#v", config, plan.Actions)
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
	if _, err := os.Lstat(filepath.Join(home, ".claude", "agents", source.GlobalAgentVariantsFile)); !os.IsNotExist(err) {
		t.Fatalf("the declaration itself was linked as an agent (err=%v)", err)
	}
	toml := filepath.Join(home, ".codex", "agents", "super-lead.toml")
	assertGlobalRoleFile(t, toml)
	content := string(readAgentFixture(t, toml))
	if !strings.Contains(content, `name = "super-lead"`) || !strings.Contains(content, `description = "Deep lead."`) ||
		!strings.Contains(content, "Lead body, byte for byte.") {
		t.Fatalf("variant TOML twin:\n%s", content)
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

// The declaration shape the professor clone ships (a variants.json beside its
// global agents) must render through the real compiler: a swap whose text has
// drifted from its source agent is an error naming the variant. The input is
// this repository's own fixture, never a path outside it, so a missing
// fixture is a failure here and not an empty loop; the clone's real file is
// rendered by that clone's gate with the built binary.
func TestShippedGlobalAgentVariantsRender(t *testing.T) {
	agentsDir := filepath.Join("testdata", "global-variants")
	raw, err := os.ReadFile(filepath.Join(agentsDir, source.GlobalAgentVariantsFile))
	if err != nil {
		t.Fatalf("variants fixture: %v", err)
	}
	var file struct {
		Variants map[string]json.RawMessage `json:"variants"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse variants fixture: %v", err)
	}
	declared := file.Variants
	if len(declared) == 0 {
		t.Fatalf("variants fixture declares no variant: the test would prove nothing")
	}
	variants, err := source.LoadGlobalAgentVariants(agentsDir, testjail.Scratch(t))
	if err != nil {
		t.Fatalf("variants fixture: %v", err)
	}
	if len(variants) != len(declared) {
		t.Fatalf("rendered %d variants, the fixture declares %d", len(variants), len(declared))
	}
	byName := map[string]string{}
	for _, variant := range variants {
		role, toml, err := renderFixtureMachineAgent(variant.Path, string(variant.Content), testjail.Scratch(t))
		if err != nil {
			t.Fatalf("variant %s does not compile to TOML: %v", variant.Name, err)
		}
		if role != variant.Name || !strings.Contains(toml, `name = "`+variant.Name+`"`) {
			t.Fatalf("variant %s compiled to role %q:\n%s", variant.Name, role, toml)
		}
		byName[variant.Name] = string(variant.Content)
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
