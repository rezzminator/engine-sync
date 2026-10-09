package wrappertest

import (
	"os"
	"testing"

	"github.com/rezzminator/engine-sync/internal/testjail"
)

// TestMain jails HOME, TMPDIR and the XDG roots (internal/testjail).
func TestMain(m *testing.M) { os.Exit(testjail.Run(m)) }
