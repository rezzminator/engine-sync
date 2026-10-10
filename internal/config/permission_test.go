package config

import (
	"encoding/json"
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
