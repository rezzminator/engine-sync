package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestPreparationProtectsForeignFileFromMergeCallback(t *testing.T) {
	path := filepath.Join(testjail.Scratch(t), "config.jsonc")
	if err := os.WriteFile(path, []byte("user-owned"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	out, _, problems := reconcile.Prepare([]artifact.Artifact{{Path: path, Merge: func([]byte) ([]byte, []string, []string) { calls++; return []byte("merged"), nil, nil }}})
	if calls != 0 || len(problems) != 0 || len(out) != 1 {
		t.Fatalf("calls=%d %+v", calls, problems)
	}
}
