package machine

import (
	"errors"
	"io"
	"testing"

	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/reconcile"
)

func TestProjectCommandsPolicyFailureIsTheStepsOwnErrorAndNeverEntersTheGuard(t *testing.T) {
	boom := errors.New("policy unreadable")
	guarded := false
	combined := diag.Report{OK: true}
	err := ProjectCommands(func(string) string { return "" }, t.TempDir(), reconcile.Plan, nil,
		func(string, string, io.Writer) (*config.Config, error) { return nil, boom },
		func([]string, bool, func() int) (int, error) { guarded = true; return 0, nil }, &combined, io.Discard, true)
	if err != boom || guarded || !combined.OK {
		t.Fatalf("err %v (want %v), guarded %v (want false), combined.OK %v (want true)", err, boom, guarded, combined.OK)
	}
}
