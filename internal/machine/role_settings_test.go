package machine

import (
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"strings"
	"testing"
)

func TestCodexRoleSettings(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fields    map[string]string
		modelMap  map[string]string
		source    string
		want      codex.Role
		wantError string
	}{
		{name: "codex-model wins", fields: map[string]string{"model": "claude-sonnet-5-5", "codex-model": "gpt-6-luna"}, source: "/a/orch.md", want: codex.Role{Model: "gpt-6-luna"}},
		{name: "unmapped alias omitted", fields: map[string]string{"model": "claude-sonnet-5-5"}, source: "/a/orch.md", want: codex.Role{}},
		{name: "inherit keeps the session model", fields: map[string]string{"model": "inherit", "effort": "high"}, source: "/a/x.md", want: codex.Role{Effort: "high"}},
		{name: "configured key maps", fields: map[string]string{"model": "fast"}, modelMap: map[string]string{"fast": "gpt-fixture"}, source: "/a/x.md", want: codex.Role{Model: "gpt-fixture"}},
		{name: "codex-effort overrides", fields: map[string]string{"model": "opus", "effort": "high", "codex-effort": "xhigh"}, source: "/a/x.md", want: codex.Role{Model: "gpt-6.1-sol", Effort: "xhigh"}},
		{name: "bad codex-effort", fields: map[string]string{"codex-effort": "max"}, source: "/a/x.md", wantError: "max"},
		{name: "workspace-write", fields: map[string]string{"tools": "Bash, Read, Grep, Glob", "codex-sandbox": "workspace-write"}, source: "/a/tracer-rr.md", want: codex.Role{Sandbox: "workspace-write"}},
		{name: "bad codex-sandbox", fields: map[string]string{"codex-sandbox": "danger-full-access"}, source: "/a/x.md", wantError: "danger-full-access"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelMap := tc.modelMap
			if modelMap == nil {
				modelMap = fixtureMachinePolicy().Codex.ModelMap
			}
			got, err := codex.RoleSettings(tc.fields, modelMap, tc.source)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.source) ||
					!strings.Contains(err.Error(), tc.wantError) ||
					got != (codex.Role{}) {
					t.Fatalf(
						"settings = %#v, %v, want no role and error naming %s and %s",
						got,
						err,
						tc.source,
						tc.wantError,
					)
				}
				if tc.name == "Claude ID refused" && !strings.Contains(err.Error(), "fable, haiku, opus, sonnet") {
					t.Fatalf("unmapped model error lacks sorted map keys: %v", err)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("settings = %#v, %v, want %#v, nil", got, err, tc.want)
			}
		})
	}
}
