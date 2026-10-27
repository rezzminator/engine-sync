package jsonc_test

import (
	"os"
	"testing"

	"github.com/rezzminator/engine-sync/internal/testjail"
)

func TestMain(m *testing.M) { os.Exit(testjail.RunTests(m)) }
