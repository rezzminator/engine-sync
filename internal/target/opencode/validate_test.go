package opencode

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeDoctorRejectsGeneratedAgentModeDrift(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "agents", "worker.md"),
		"---\ndescription: Worker role.\nmodel: sonnet\n---\nWork.\n",
	)
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("build result=%#v err=%v", result, err)
	}
	path := filepath.Join(root, ".opencode", "agent", "worker.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeOpenCodeFixture(t, path, strings.Replace(string(raw), "mode: all", "mode: subagent", 1))

	doctor, err := Compile(Options{Root: root, Home: home, Mode: ModeDoctor})
	joined := strings.Join(doctor.Problems, "\n")
	if err != nil || doctor.OK || !strings.Contains(joined, "INVALID "+path) ||
		!strings.Contains(joined, `mode "subagent"`) {
		t.Fatalf("doctor result=%#v err=%v", doctor, err)
	}
}

// A machine agent under the global agents/ dir with no description is a named
// problem, and a header that does not parse is FRONTMATTER-INVALID.
func TestOpenCodeValidateNamesMachineAgentsAndHeaderFailures(t *testing.T) {
	root := testjail.Scratch(t)
	silent := filepath.Join(root, "agents", "worker.md")
	broken := filepath.Join(root, "agent", "bad.md")
	diagnostics := Target{}.Validate([]artifact.Artifact{
		{Path: silent, Content: []byte("---\nmode: subagent\n---\nWork.\n")},
		{Path: broken, Content: []byte("---\ndescription: Reader\nengines:\n  codex: [unclosed\n---\nWork.\n")},
	})
	var messages []string
	for _, d := range diagnostics {
		messages = append(messages, d.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, silent+" — empty description") || !strings.Contains(joined, "FRONTMATTER-INVALID "+broken+":") {
		t.Fatalf("diagnostics %q, want the silent machine agent and the broken header named", joined)
	}
}
