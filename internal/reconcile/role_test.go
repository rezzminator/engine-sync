package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"testing"
)

func TestMachineRoleRepairPublishesRegularFile(t *testing.T) {
	path := filepath.Join(testjail.Scratch(t), "role.toml")
	content := []byte(artifact.Marker(artifact.TOML, "source") + "\nname = \"reader\"\n")
	state, _, err := reconcile.ClassifyGlobalRole(path, content, nil)
	if err != nil || state != reconcile.GlobalRoleMissing {
		t.Fatal(state, err)
	}
	if err := reconcile.ApplyGlobalRole(path, content, state); err != nil {
		t.Fatal(err)
	}
	got, err := reconcile.ReadGlobalRoleFile(path)
	if err != nil || string(got) != string(content) {
		t.Fatalf("%q %v", got, err)
	}
}
