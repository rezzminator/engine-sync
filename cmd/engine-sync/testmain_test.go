package main

import (
	"os"
	"testing"

	"github.com/rezzminator/engine-sync/internal/testjail"
)

// TestMain jails HOME, TMPDIR and the XDG roots before any test resolves a
// path (internal/testjail).
func TestMain(m *testing.M) { os.Exit(testjail.Run(m)) }
