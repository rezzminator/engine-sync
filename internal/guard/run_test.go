package guard

import (
	"errors"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"testing"
	"time"
)

func TestRunRecordsPartialWritesAndDeduplicates(t *testing.T) {
	root, state := testjail.Scratch(t), testjail.Scratch(t)
	rc, err := WithWriterGuard(state, []string{root, filepath.Join(root, ".")}, "0.2.0-alpha", func() string { return "fixture" }, true, time.Second, func() int { return 1 })
	if err != nil || rc != 1 {
		t.Fatalf("%d %v", rc, err)
	}
	if err := Check(state, root, "0.1.0"); !errors.Is(err, ErrNewerWriter) {
		t.Fatalf("partial writer lost: %v", err)
	}
}
