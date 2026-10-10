package opencode

import (
	"encoding/json"
	"github.com/rezzminator/engine-sync/internal/config"
	"testing"
)

func TestNativePermissionForms(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"whole policy action", `"ask"`, `"ask"`},
		{"ordered tool patterns", `{"edit":{"*":"deny","src/**":"allow","src/private/**":"ask"},"bash":"ask"}`, `{"edit":{"*":"deny","src/**":"allow","src/private/**":"ask"},"bash":"ask"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var permission config.Permission
			if err := json.Unmarshal([]byte(tc.input), &permission); err != nil {
				t.Fatalf("native permission: %v", err)
			}
			raw, err := json.Marshal(permission)
			if err != nil || string(raw) != tc.want {
				t.Fatalf("round trip=%s err=%v want=%s", raw, err, tc.want)
			}
		})
	}
}
