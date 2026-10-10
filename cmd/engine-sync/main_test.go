package main

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/cli"
	"strings"
	"testing"
)

func TestProcessAdapterUsageExit(t *testing.T) {
	var out, err bytes.Buffer
	rc := cli.Main([]string{"unknown"}, &out, &err, strings.NewReader(""), version)
	if rc != 2 || !strings.Contains(err.String(), "unknown command") {
		t.Fatalf("%d %s", rc, err.String())
	}
}
