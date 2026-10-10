package codex

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListedProjectWorkbenchProbeFailure(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "Root.\n")
	project := strings.Repeat("x", 300)
	writeCodexFixture(t, filepath.Join(root, ".claude", "engine-sync.json"),
		`{"version":1,"workbenchMarker":".professor/workbench.json","codex":{"projects":["`+project+`"]}}`)
	result, err := Build(Options{Root: root, Home: home})
	want := "inspect workbench " + filepath.Join(root, project, ".professor") + ": "
	if err != nil || result.OK || result.Wrote != 0 || len(result.Problems) != 1 ||
		!strings.HasPrefix(result.Problems[0], want) {
		t.Fatalf("build = %#v, %v, want zero writes and problem starting %q", result, err, want)
	}
}

func TestWorkbenchParentProjects(t *testing.T) {
	for _, listed := range []bool{false, true} {
		name := "scan"
		if listed {
			name = "listed"
		}
		t.Run(name, func(t *testing.T) {
			root, home := testjail.Scratch(t), testjail.Scratch(t)
			writeCodexFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"workbenchMarker":".professor/workbench.json"}`)
			writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "Parent.\n")
			writeCodexFixture(t, filepath.Join(root, "api", "CLAUDE.md"), "API.\n")
			writeCodexFixture(t, filepath.Join(root, "scribe", "CLAUDE.md"), "Scribe.\n")
			writeCodexFixture(t, filepath.Join(root, "scribe", ".professor", "workbench.json"), `{}`)
			writeCodexFixture(
				t,
				filepath.Join(root, "scribe", ".claude", "agents", "clerk.md"),
				"---\ndescription: Clerk.\n---\nClerk.\n",
			)
			if listed {
				if err := os.Remove(filepath.Join(root, ".claude", "engine-sync.json")); err != nil {
					t.Fatal(err)
				}
				writeCodexFixture(
					t,
					filepath.Join(root, ".claude", "engine-sync.json"),
					`{"version":1,"workbenchMarker":".professor/workbench.json","codex":{"projects":["scribe"]}}`,
				)
			}
			result, err := Build(Options{Root: root, Home: home})
			if err != nil || !result.OK {
				t.Fatalf("build=%#v err=%v", result, err)
			}
			for _, path := range []string{filepath.Join(root, "scribe", "AGENTS.md"), filepath.Join(root, ".codex", "agents", "clerk-scribe.toml")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("parent compiled workbench %s: %v", path, err)
				}
			}
			if listed {
				want := "scribe is a workbench: it builds as its own root, not as a child project"
				if strings.Join(result.Warnings, "\n") != want {
					t.Errorf("warnings=%q, want %q", result.Warnings, want)
				}
			} else if _, err := os.Stat(filepath.Join(root, "api", "AGENTS.md")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
