package config

import (
	"encoding/json"
	"path"
	"strings"
	"testing"
)

func TestPermissionRoundTripPreservesRuleOrder(t *testing.T) {
	raw := []byte(`{"bash":{"*":"allow","git push *":"deny","git status *":"allow"}}`)
	var p Permission
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(p)
	if err != nil || string(out) != string(raw) {
		t.Fatalf("%s %v", out, err)
	}
}

func TestPermissionLayerReplacesAToolInPlace(t *testing.T) {
	var permission Permission
	if err := json.Unmarshal([]byte(`{"*":"allow","bash":{"git push":"deny"},"edit":{"CLAUDE.md":"deny"}}`), &permission); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"*":"ask","read":"allow"}`), &permission); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range permission.Tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, ","); got != "*,bash,edit,read" || permission.Tools[0].Value.Action != "ask" {
		t.Fatalf("layered tools = %s, first %#v; want *,bash,edit,read with * ask", got, permission.Tools[0])
	}
}

func TestAuditPermissionLayers(t *testing.T) {
	for _, tc := range []struct{ base, overlay, tool, value, want string }{
		{`{"*":"allow","bash":{"*":"allow","git push*":"deny"}}`, `{"bash":{"git status":"allow"}}`, "bash", "git push origin", "deny"},
		{`{"bash":{"git push*":"deny"}}`, `{"bash":"allow"}`, "bash", "git push origin", "deny"},
		{`{"bash":{"git push*":"deny"}}`, `{"bash":{"git push*":"allow"}}`, "bash", "git push origin", "deny"},
		{`{"bash":{"git status":"allow"}}`, `{"bash":{"*":"deny"}}`, "bash", "git status", "allow"},
		{`{"bash":{"git *":"allow"}}`, `{"bash":{"git push*":"deny"}}`, "bash", "git push", "deny"},
		{`{"bash":{"git push*":"deny"}}`, `"allow"`, "bash", "git push", "deny"},
	} {
		t.Run(tc.base+tc.overlay, func(t *testing.T) {
			var p Permission
			for _, layer := range []string{tc.base, tc.overlay} {
				if err := json.Unmarshal([]byte(layer), &p); err != nil {
					t.Fatal(err)
				}
			}
			answer := "deny"
			if p.Action != "" {
				answer = p.Action
			}
			for _, tool := range p.Tools {
				if tool.Name != "*" && tool.Name != tc.tool {
					continue
				}
				if tool.Value.Action != "" {
					answer = tool.Value.Action
				}
				for _, rule := range tool.Value.Rules {
					if matched, _ := path.Match(rule.Pattern, tc.value); matched {
						answer = rule.Action
					}
				}
			}
			if answer != tc.want {
				t.Fatalf("permission=%#v action=%s want=%s", p, answer, tc.want)
			}
		})
	}
	for _, p := range []Permission{{}, Default().OpenCode.Permission} {
		raw, err := json.Marshal(p)
		if err != nil || string(raw) != `"deny"` {
			t.Errorf("absent policy=%s %v", raw, err)
		}
	}
}
