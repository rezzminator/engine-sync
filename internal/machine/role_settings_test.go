package machine

import (
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"strings"
	"testing"
)

// TestCodexRoleSettings pins R1.4's precedence: engines.codex.model (a
// codex.modelMap key mapped, any other value verbatim), then modelMap[model];
// engines.codex.effort, then effort, max reaching Codex as xhigh;
// engines.codex.sandbox, else read-only inferred from tools.
func TestCodexRoleSettings(t *testing.T) {
	for _, tc := range []struct {
		name, header, warning string
		modelMap              map[string]string
		want                  codex.Role
	}{
		{name: "engines model verbatim", header: "model: claude-sonnet-5-5\nengines:\n  codex:\n    model: gpt-6-luna", want: codex.Role{Model: "gpt-6-luna"}},
		{name: "engines model mapped", header: "model: sonnet\nengines:\n  codex:\n    model: opus", want: codex.Role{Model: "gpt-6.1-sol"}},
		{name: "unmapped alias omitted", header: "model: claude-sonnet-5-5", want: codex.Role{}, warning: `unmapped Codex model alias "claude-sonnet-5-5" in /a/x.md — model omitted`},
		{name: "inherit keeps the session model", header: "model: inherit\neffort: high", want: codex.Role{Effort: "high"}},
		{name: "configured key maps", header: "model: fast", modelMap: map[string]string{"fast": "gpt-fixture"}, want: codex.Role{Model: "gpt-fixture"}},
		{name: "engines effort overrides", header: "model: opus\neffort: high\nengines:\n  codex:\n    effort: xhigh", want: codex.Role{Model: "gpt-6.1-sol", Effort: "xhigh"}},
		{name: "effort max is xhigh", header: "effort: max", want: codex.Role{Effort: "xhigh"}},
		{name: "engines effort max is xhigh", header: "effort: low\nengines:\n  codex:\n    effort: max", want: codex.Role{Effort: "xhigh"}},
		{name: "workspace-write", header: "tools: Bash, Read, Grep, Glob\nengines:\n  codex:\n    sandbox: workspace-write", want: codex.Role{Sandbox: "workspace-write"}},
		{name: "sandbox inherit writes no line", header: "tools: Read\nengines:\n  codex:\n    sandbox: inherit", want: codex.Role{}},
		{name: "read-only from tools", header: "tools: Read, Grep", want: codex.Role{Sandbox: "read-only"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelMap := tc.modelMap
			if modelMap == nil {
				modelMap = fixtureMachinePolicy().Codex.ModelMap
			}
			front, _, err := source.Parse("---\n" + tc.header + "\n---\nBody.\n")
			if err != nil {
				t.Fatal(err)
			}
			got, warning, err := codex.RoleSettings(front, modelMap, "/a/x.md")
			if err != nil || got != tc.want || warning != tc.warning {
				t.Fatalf("settings = %#v, %q, %v, want %#v, %q", got, warning, err, tc.want, tc.warning)
			}
			if tc.warning == "" && strings.Contains(warning, "unmapped") {
				t.Fatalf("unexpected warning %q", warning)
			}
		})
	}
}
