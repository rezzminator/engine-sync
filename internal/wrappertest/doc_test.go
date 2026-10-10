package wrappertest

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"testing"
)

func TestTheOverrideRunsAndIsNeverLinked(t *testing.T) {
	r := newRig(t, "")
	r.cache(t, version)
	override := filepath.Join(testjail.Scratch(t), "dev-engine-sync")
	writeFile(t, override, fakeBinary, 0o755)
	out, stderr, code := r.run(t, "", []string{"ENGINE_SYNC_BIN=" + override, "FAKE_NAME=override"}, "codex", "check", ".")
	if code != 0 || out != "ran override args=codex check .\n" {
		t.Fatalf("code %d, stdout %q, stderr %q: $ENGINE_SYNC_BIN runs first", code, out, stderr)
	}
	if got := r.linkTarget(t); got != "" {
		t.Fatalf("the stable link names %q: an override is never linked", got)
	}
}
