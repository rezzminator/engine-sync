package cli

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"testing"
)

// TestMain jails HOME, TMPDIR and the XDG roots before any test resolves a
// path (internal/testjail).
func TestMain(m *testing.M) { os.Exit(testjail.RunTests(m)) }
