package source

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"testing"
)

func TestRoleNamePolicy(t *testing.T) {
	cfg := config.Default().Codex
	cfg.SuffixMode = "strip-prefix"
	cfg.SuffixPrefix = "pkg/"
	cfg.NeverRegister = []string{"reader-secret"}
	for _, tc := range []struct {
		base, project, want string
		register            bool
	}{{"reader", "pkg/api", "reader-api", true}, {"reader", "pkg/secret", "reader-secret", false}} {
		got, register, err := RoleName(tc.base, tc.project, cfg)
		if err != nil || got != tc.want || register != tc.register {
			t.Errorf("%#v got %s/%v %v", tc, got, register, err)
		}
	}
	if _, _, err := RoleName("../escape", ".", cfg); err == nil {
		t.Fatal("path name admitted")
	}
}
