package codex

import (
	"github.com/rezzminator/engine-sync/internal/config"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"strings"
	"testing"
)

func TestNeutralRoleUsesEngineModelDefault(t *testing.T) {
	role, warning, e := codexRoleSettings(claudesource.Frontmatter{Model: "sonnet", Fields: map[string]string{"model": "sonnet"}}, config.Default().Codex.ModelMap, "role.md")
	if e != nil || role.Model != "" || !strings.Contains(warning, `unmapped Codex model alias "sonnet"`) {
		t.Fatalf("unmapped neutral role=%#v err=%v", role, e)
	}
}
