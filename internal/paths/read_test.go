package paths_test

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegularAndHeaderLimits(t *testing.T) {
	path := filepath.Join(testjail.Scratch(t), "source")
	raw := bytes.Repeat([]byte("x"), paths.MaxSourceBytes+1)
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := paths.ReadRegular(path); e == nil {
		t.Fatal("oversized source accepted")
	}
	if got, e := paths.ReadHeader(path); e != nil || len(got) != 4097 {
		t.Fatalf("bounded header len=%d error=%v", len(got), e)
	}
}
