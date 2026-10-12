package machine

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

// A source missing a required frontmatter field is a named problem and is
// skipped; a healthy agent beside it still compiles.
func TestGlobalAgentsMissingFrontmatterFieldIsSkippedAndNamed(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "broken.md"),
		"---\nname: broken\n---\n\nno description field.\n")
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil || !strings.Contains(strings.Join(result.Problems, "\n"), "needs both name: and description") {
		t.Fatalf("RunGlobalAgents: problems %v (%v), want the frontmatter error named", result.Problems, err)
	}
	if len(result.Compiled) != 1 || filepath.Base(result.Compiled[0].Path) != "alpha.toml" {
		t.Fatalf("compiled = %+v, want alpha alone", result.Compiled)
	}
}

// The build pass re-enters RunGlobalAgents with the plan's source, which
// already carries the plan's skips: each broken agent is still named once.
func TestGlobalAgentsReentryNamesABrokenAgentOnce(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "broken.md"),
		"---\nname: broken\n---\n\nno description field.\n")
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")
	options := GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home}
	plan, err := runFixtureMachineAgents(options)
	if err != nil {
		t.Fatalf("plan pass: %v", err)
	}
	options.Source = plan.Source
	build, err := runFixtureMachineAgents(options)
	if n := strings.Count(strings.Join(build.Problems, "\n"), "needs both name: and description"); err != nil || n != 1 {
		t.Fatalf("build pass problems %q (%v): the broken agent named %d times, want once", build.Problems, err, n)
	}
	if got := strings.Join(build.Source.Failed, ","); got != "broken" {
		t.Fatalf("Failed = %q, want broken once", got)
	}
}
