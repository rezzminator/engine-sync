package wrappertest

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"testing"
)

// TestMain jails HOME, TMPDIR and the XDG roots (internal/testjail).
func TestMain(m *testing.M) { os.Exit(testjail.RunTests(m)) }
