package paths_test

import (
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testjail.RunTests(m)) }
func TestLocationsRespectAbsoluteLayers(t *testing.T) {
	home := testjail.Scratch(t)
	get := func(k string) string {
		if k == "ENGINE_SYNC_STATE" {
			return filepath.Join(home, "chosen")
		}
		if k == "XDG_CONFIG_HOME" || k == "CLAUDE_CONFIG_DIR" {
			return "relative"
		}
		return ""
	}
	if paths.EngineSyncState(get, home) != filepath.Join(home, "chosen") || paths.EngineSyncConfig(home) != filepath.Join(home, ".claude", "engine-sync.json") {
		t.Fatal("layer resolution")
	}
}
