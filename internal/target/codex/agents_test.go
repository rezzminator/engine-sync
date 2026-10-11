package codex

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"testing"
)

func TestNeutralRoleUsesEngineModelDefault(t *testing.T) {
	role, e := codexRoleSettings(map[string]string{"model": "sonnet"}, config.Default().Codex.ModelMap, "role.md")
	if e != nil || role.Model != "" {
		t.Fatalf("unmapped neutral role=%#v err=%v", role, e)
	}
}
