package opencode

import (
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
