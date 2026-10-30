package runlog_test

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testjail.RunTests(m)) }
