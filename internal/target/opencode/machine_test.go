package opencode

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"testing"
)

func TestMachineCommandDestination(t *testing.T) {
	home := testjail.Scratch(t)
	writeOpenCodeFixture(t, filepath.Join(home, ".claude", "commands", "hello.md"), "---\ndescription: Hello.\n---\nHi.\n")
	cfg := config.Default()
	p := source.LoadMachineCommands(home, home, false, cfg)
	outputs, ds := renderOpenCodeMachine(p, cfg)
	if len(ds) != 0 || len(outputs) != 1 || outputs[0].Path != filepath.Join(home, ".config", "opencode", "command", "hello.md") {
		t.Fatalf("outputs=%#v diagnostics=%#v", outputs, ds)
	}
}
