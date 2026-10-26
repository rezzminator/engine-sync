package codex

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
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
		`{"version":1,"workbenchMarker":".example/workbench.json","codex":{"projects":["`+project+`"]}}`)
	result, err := Build(Options{Root: root, Home: home})
	want := "inspect workbench " + filepath.Join(root, project, ".example") + ": "
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
			writeCodexFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"workbenchMarker":".example/workbench.json"}`)
			writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "Parent.\n")
			writeCodexFixture(t, filepath.Join(root, "api", "CLAUDE.md"), "API.\n")
			writeCodexFixture(t, filepath.Join(root, "scribe", "CLAUDE.md"), "Scribe.\n")
			writeCodexFixture(t, filepath.Join(root, "scribe", ".example", "workbench.json"), `{}`)
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
					`{"version":1,"workbenchMarker":".example/workbench.json","codex":{"projects":["scribe"]}}`,
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

func TestNestedListedProjects(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "Root.\n")
	nested := []string{"src/api", "src/web/app"}
	for _, project := range nested {
		writeCodexFixture(t, filepath.Join(root, project, "CLAUDE.md"), "Child "+project+".\n")
	}
	writeCodexFixture(t, filepath.Join(root, "src", "api", ".claude", "agents", "clerk.md"), "---\ndescription: Clerk.\n---\nClerk.\n")
	writeCodexFixture(t, filepath.Join(root, ".claude", "engine-sync.json"),
		`{"version":1,"codex":{"projects":["src/api","src/web/app","src/skip"],"excludeProjects":["src/skip"]}}`)
	result, err := Build(Options{Root: root, Home: home})
	if err != nil || !result.OK {
		t.Fatalf("build = %#v, %v", result, err)
	}
	for _, project := range nested {
		raw, err := os.ReadFile(filepath.Join(root, project, "AGENTS.md"))
		if err != nil || !strings.Contains(string(raw), artifact.Line(project+"/CLAUDE.md")) || !strings.Contains(string(raw), "Child "+project+".") {
			t.Errorf("%s/AGENTS.md = %q, %v; want the generated marker and the child body", project, raw, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".codex", "agents", "clerk-src-api.toml")); err != nil {
		t.Errorf("nested project role: %v", err)
	}
	if check, err := CheckProject(Options{Root: root, Home: home}); err != nil || !check.OK {
		t.Fatalf("check after build = %#v, %v", check, err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "web", "app", "AGENTS.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if check, err := CheckProject(Options{Root: root, Home: home}); err != nil || check.OK {
		t.Fatalf("check passed over an edited nested AGENTS.md: %#v, %v", check, err)
	}
}

func TestNestedListedProjectRefusals(t *testing.T) {
	outside := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(outside, "CLAUDE.md"), "Outside.\n")
	for _, tc := range []struct{ name, project, want string }{
		{"absolute", outside, `codex.projects "` + outside + `" must be relative and stay inside its root`},
		{"parent", "src/../../x", `codex.projects "src/../../x" must be relative and stay inside its root`},
		{"dotdot", "..", `codex.projects ".." must be relative and stay inside its root`},
		{"symlink out", "src/link", "does not resolve below the root"},
		{"no orientation", "src/empty", "no regular CLAUDE.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, home := testjail.Scratch(t), testjail.Scratch(t)
			writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "Root.\n")
			if err := os.MkdirAll(filepath.Join(root, "src", "empty"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, "src", "link")); err != nil {
				t.Fatal(err)
			}
			writeCodexFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"codex":{"projects":["`+tc.project+`"]}}`)
			result, err := Build(Options{Root: root, Home: home})
			got := strings.Join(result.Problems, "\n")
			if err != nil {
				got = err.Error()
			}
			if result.OK || !strings.Contains(got, tc.want) {
				t.Fatalf("build = %#v, %v; want a refusal containing %q", result, err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(outside, "AGENTS.md")); !os.IsNotExist(err) {
				t.Errorf("wrote outside the root: %v", err)
			}
		})
	}
}
