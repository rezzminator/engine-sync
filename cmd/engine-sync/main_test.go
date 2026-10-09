package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rezzminator/engine-sync/internal/guard"
)

// TestGuardedChecksTheWriterRecordUnderTheLock: a writer that waits for the
// root's lock while a newer binary writes that root (two plugin seats of
// different versions ending a turn in one repository) is refused once it holds
// the lock, and never runs its write over the newer output.
func TestGuardedChecksTheWriterRecordUnderTheLock(t *testing.T) {
	home := t.TempDir()
	vars := map[string]string{"HOME": home}
	e := env{get: func(name string) string { return vars[name] }}
	state, root := e.state(home), filepath.Join(home, "repo")
	release, err := guard.Lock(state, root)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		code int
		err  error
		ran  bool
	}
	done := make(chan outcome, 1)
	go func() {
		ran := false
		code, err := guarded(e, []string{root}, true, func() int { ran = true; return 0 })
		done <- outcome{code, err, ran}
	}()
	// Give the older writer time to reach the lock the newer one holds.
	time.Sleep(200 * time.Millisecond)
	if err := guard.Save(state, root, "99.0.0", "/newer/engine-sync"); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.ran || got.code != 1 || !errors.Is(got.err, guard.ErrNewerWriter) {
		t.Fatalf("waiting older writer: ran=%v code=%d err=%v, want refused with NEWER-WRITER before its write", got.ran, got.code, got.err)
	}
}
