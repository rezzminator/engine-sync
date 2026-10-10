package codex

import (
	"testing"
)

func TestNeutralRoleUsesEngineModelDefault(t *testing.T) {
	role, e := codexRoleSettings(map[string]string{"model": "sonnet"}, defaultConfig().ModelMap, "role.md")
	if e != nil || role.Model != "" {
		t.Fatalf("unmapped neutral role=%#v err=%v", role, e)
	}
}
