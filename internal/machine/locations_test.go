package machine

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"testing"
)

func TestMachineHomeResolvesToAbsolutePath(t *testing.T) {
	home := testjail.Scratch(t)
	got, err := resolveMachineHome(filepath.Join(home, "one", ".."))
	if err != nil || got != home {
		t.Fatalf("%q %v", got, err)
	}
}
