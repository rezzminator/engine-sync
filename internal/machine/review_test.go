package machine

import (
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

const codeReviewSlot = "{effort}"

const codeReviewCommand = "/code-review"

var reviewTestModel = fixtureMachinePolicy().Codex.ModelMap["opus"]

func machineReviewWant(effort string) string {
	return codex.ReviewShellCommand(reviewTestModel, effort)
}

// Door 2 for the lander's own road: a machine-global role compiled by
// RunGlobalAgents. A lander whose compiled body carries the task-scoped prompt
// reviews the wrong diff on Codex.
func TestCompiledGlobalRoleCarriesTheFlightScopedReview(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(
		t,
		filepath.Join(home, ".professor", "templates", "global", "agents", "gate.md"),
		"---\nname: gate\ndescription: Gates one flight.\ntools: Read\nmodel: opus\n---\n\n"+
			"Run `/code-review "+codeReviewSlot+"` over the flight's diff.\n",
	)
	if _, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, ".professor"), Home: home}); err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	path := filepath.Join(filepath.Join(home, ".codex", "agents"), "gate.toml")
	got := string(readAgentFixture(t, path))
	if strings.Contains(got, codeReviewCommand) {
		t.Fatalf("%s still spells %s:\n%s", path, codeReviewCommand, got)
	}
	want := codex.ReviewShellCommand(reviewTestModel, codeReviewSlot)
	escaped := strings.ReplaceAll(want, `"`, `\"`)
	if !strings.Contains(got, want) && !strings.Contains(got, escaped) {
		t.Fatalf("%s carries no flight-scoped review at the effort slot:\n%s", path, got)
	}
	if err := codex.ValidateRole(got); err != nil {
		t.Fatalf("%s does not parse as TOML with the slot review embedded: %v", path, err)
	}
}

// Doors 1 and 2 from the files they wrote: a compiled project command, a
// compiled project role (body AND description), the compiled AGENTS.md, and a
// compiled machine-global role. A Codex artifact still spelling /code-review
// sends its seat to Codex's whole-branch /review.
func TestEveryCompiledCodexArtifactCarriesTheShellReview(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n\nClose with `/code-review low`.\n")
	writeAgentFixture(
		t,
		filepath.Join(root, ".claude", "agents", "dev.md"),
		"---\nname: dev\ndescription: Builds, then runs /code-review low.\ntools: Read\nmodel: sonnet\n---\n\n"+
			"Your last step is `/code-review high` over your own change.\n",
	)
	writeAgentFixture(
		t,
		filepath.Join(root, ".claude", "commands", "land.md"),
		"---\ndescription: Lands a change; closes with /code-review medium.\n---\n\nRun `/code-review max`.\n",
	)
	writeAgentFixture(
		t,
		filepath.Join(home, ".professor", "templates", "global", "agents", "gamma.md"),
		"---\nname: gamma\ndescription: Global role; reviews with /code-review low.\ntools: Read\nmodel: sonnet\n"+
			"---\n\nClose with `/code-review xhigh`.\n",
	)
	policy := fixtureMachinePolicy()
	if result, err := codex.RunProject(codex.Options{Root: root, Home: home, Mode: codex.ModeBuild, Config: &policy}); err != nil || !result.OK {
		t.Fatalf("build: result=%#v err=%v", result, err)
	}
	if _, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, ".professor"), Home: home}); err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	cases := []struct {
		name    string
		path    string
		efforts []string
	}{
		{name: "AGENTS.md", path: filepath.Join(root, "AGENTS.md"), efforts: []string{"low"}},
		{
			name:    "project role",
			path:    filepath.Join(root, ".codex", "agents", "dev.toml"),
			efforts: []string{"low", "high"},
		},
		{
			name:    "project command",
			path:    filepath.Join(root, ".codex", "skills", "land", "SKILL.md"),
			efforts: []string{"medium", "xhigh"},
		},
		{
			name:    "global role",
			path:    filepath.Join(filepath.Join(home, ".codex", "agents"), "gamma.toml"),
			efforts: []string{"low", "xhigh"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := string(readAgentFixture(t, testCase.path))
			if strings.Contains(got, codeReviewCommand) {
				t.Fatalf("%s still spells %s — the seat would run Codex's whole-branch review:\n%s",
					testCase.path, codeReviewCommand, got)
			}
			for _, effort := range testCase.efforts {
				// A role's description is a TOML basic string: its quotes arrive escaped.
				escaped := strings.ReplaceAll(machineReviewWant(effort), `"`, `\"`)
				if !strings.Contains(got, machineReviewWant(effort)) && !strings.Contains(got, escaped) {
					t.Fatalf("%s carries no %s review at effort %q:\n%s", testCase.path, "codex review -c", effort, got)
				}
			}
			if strings.HasSuffix(testCase.path, ".toml") {
				if err := codex.ValidateRole(got); err != nil {
					t.Fatalf("%s does not parse as TOML with the review command embedded: %v", testCase.path, err)
				}
			}
		})
	}
}
