package opencode

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeAgentRelativePathFailureIsAProblemAndSkipsTheAgent(t *testing.T) {
	root := testjail.Scratch(t)
	var problems []string
	added := 0
	compileOpenCodeAgent(
		root,
		"worker",
		filepath.Join("relative", "worker.md"),
		nil,
		nil,
		nil,
		func(artifact.Artifact) { added++ },
		func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) },
		func(string, ...any) {}, nil,
	)
	want := "relative path of " + filepath.Join("relative", "worker.md") + ": "
	if added != 0 || !containsProblem(problems, want) {
		t.Fatalf("added=%d problems=%q, want the entry skipped and %q", added, problems, want)
	}
}
func TestOpenCodeNativeOverridesAndTemplateSource(t *testing.T) {
	root := testjail.Scratch(t)
	path := filepath.Join(root, ".claude", "agents", "reader.md")
	writeOpenCodeFixture(t, path, "---\ndescription: Read.\nmodel: tiny\nengines:\n  opencode:\n    model: native/pinned\n    mode: primary\n  codex:\n    model: ignored\n---\nRead files.\n")
	cfg := config.Default()
	cfg.OpenCode.AgentPreamble = "Source {{.Source}}; engine {{.Engine}}; role {{.Name}}.\n"
	model := claudesource.LoadProject(root, filepath.Join(root, "home"), cfg)
	artifacts, diagnostics := (Target{}).Render(model, cfg)
	for _, d := range diagnostics {
		if d.Severity != diag.Warning {
			t.Fatal(d)
		}
	}
	found := false
	for _, a := range artifacts {
		if a.Path == filepath.Join(root, ".opencode", "agent", "reader.md") {
			found = true
			raw := string(a.Content)
			for _, want := range []string{`model: "native/pinned"`, "mode: primary", "Source .claude/agents/reader.md; engine OpenCode; role reader."} {
				if !strings.Contains(raw, want) {
					t.Errorf("missing %q in %s", want, raw)
				}
			}
		}
	}
	if !found {
		t.Fatal("reader artifact missing")
	}
	if got := (Target{}).Validate(artifacts); len(got) != 0 {
		t.Fatalf("validation: %+v", got)
	}
}

func TestOpenCodeAgentToolsRewritesAreSeparateFromProse(t *testing.T) {
	front, body, err := claudesource.Parse("---\ndescription: Read with mcp__plugin_fixture_tools__lookup.\ntools: Read, mcp__plugin_fixture_tools__lookup, mcp__plugin_fixture_tools__*\n---\nRead with mcp__plugin_fixture_tools__lookup.\n")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Rewrites = []config.Rewrite{
		{Engines: []string{"opencode"}, Kinds: []string{"agent-tools"}, Literal: "mcp__plugin_fixture_tools__", Replace: "mcp__tools-hub__"},
		{Engines: []string{"opencode"}, Kinds: []string{"agent"}, Literal: "mcp__plugin_fixture_tools__", Replace: "tools-hub_"},
		{Engines: []string{"opencode"}, Kinds: []string{"agent"}, Literal: "Read", Replace: "Inspect"},
		{Engines: []string{"codex"}, Kinds: []string{"agent-tools"}, Literal: "Read", Replace: "WrongEngine"},
	}
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global=%t", global), func(t *testing.T) {
			var outputs []artifact.Artifact
			report := func(format string, args ...any) { t.Errorf(format, args...) }
			renderOpenCodeAgent(openCodeAgentSpec{
				Name: "worker", Path: "worker.md", Marker: "worker.md", Output: "worker.md", Global: global,
			}, front, body, nil, nil, []string{"tools-hub"}, cfg.Rewrites,
				func(a artifact.Artifact) { outputs = append(outputs, a) }, report, report, cfg)
			if len(outputs) != 1 {
				t.Fatalf("outputs=%#v, want one rendered agent", outputs)
			}
			raw := string(outputs[0].Content)
			for _, want := range []string{"  tools-hub_*: true\n", "Inspect with tools-hub_lookup."} {
				if !strings.Contains(raw, want) {
					t.Errorf("agent misses %q: %s", want, raw)
				}
			}
			if strings.Contains(raw, "  read: false\n") {
				t.Errorf("prose rewrite changed the Read permission: %s", raw)
			}
		})
	}
}
