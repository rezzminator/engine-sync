package opencode

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestOpenCodeDanglingCommandKeepsItsTwin(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	source := filepath.Join(root, ".claude", "commands", "lab.md")
	twin := filepath.Join(root, ".opencode", "command", "lab.md")
	writeOpenCodeFixture(t, source, "---\ndescription: Lab.\n---\nLab.\n")
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("seed=%#v err=%v", result, err)
	}
	seeded, err := os.ReadFile(twin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	target := "../../vendor/lab.md"
	if err := os.Symlink(target, source); err != nil {
		t.Fatal(err)
	}
	result, err = Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	body, readErr := os.ReadFile(twin)
	warning := "source unresolvable: " + source + " → " + target + "; output kept"
	if err != nil || !result.OK || result.Deleted != 0 || readErr != nil || !bytes.Equal(body, seeded) ||
		!containsProblem(result.Warnings, warning) {
		t.Fatalf("build=%#v err=%v body=%q read=%v, want kept twin warning %q", result, err, body, readErr, warning)
	}
	result, err = Compile(Options{Root: root, Home: home, Mode: ModeCheck})
	if err != nil || result.OK || !containsProblem(result.Problems, "DANGLING "+source) {
		t.Fatalf("check=%#v err=%v", result, err)
	}
}

// TestOpenCodeSourceThatDoesNotParseKeepsItsTwins: an agent or command whose
// header stops parsing is named FRONTMATTER-INVALID and keeps the twin it
// built before — an agent's under the name its `name:` line still declares —
// never planned for deletion as an orphan.
func TestOpenCodeSourceThatDoesNotParseKeepsItsTwins(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	agent := filepath.Join(root, ".claude", "agents", "lab.md")
	command := filepath.Join(root, ".claude", "commands", "ship.md")
	writeOpenCodeFixture(t, agent, "---\nname: lab-pro\ndescription: Lab.\n---\nLab.\n")
	writeOpenCodeFixture(t, command, "---\ndescription: Ship.\n---\nShip.\n")
	if result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild}); err != nil || !result.OK {
		t.Fatalf("seed=%#v err=%v", result, err)
	}
	twins := map[string]string{
		agent:   filepath.Join(root, ".opencode", "agent", "lab-pro.md"),
		command: filepath.Join(root, ".opencode", "command", "ship.md"),
	}
	broken := "description: Broken.\nengines:\n\topencode: false\n---\nBody.\n"
	writeOpenCodeFixture(t, agent, "---\nname: lab-pro\n"+broken)
	writeOpenCodeFixture(t, command, "---\n"+broken)
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeCheck})
	if err != nil {
		t.Fatal(err)
	}
	for source, twin := range twins {
		for _, action := range result.Actions {
			if action.Path == twin {
				t.Errorf("check plans %s %s, want the twin kept", action.Kind, twin)
			}
		}
		if want := source + " did not build; output kept: " + twin; !slices.Contains(result.Warnings, want) {
			t.Errorf("warnings %q, want %q", result.Warnings, want)
		}
		if !containsProblem(result.Problems, "FRONTMATTER-INVALID "+source+":") {
			t.Errorf("problems %q, want %s named FRONTMATTER-INVALID", result.Problems, source)
		}
	}
}
