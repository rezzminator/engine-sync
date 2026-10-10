package main

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/cli"
	"strings"
	"testing"
)

func TestReleaseMetadataReachesVersionCommand(t *testing.T) {
	var out, err bytes.Buffer
	rc := cli.Main([]string{"version"}, &out, &err, strings.NewReader(""), version)
	if rc != 0 || out.String() != "engine-sync "+version+"\n" {
		t.Fatalf("%d %q", rc, out.String())
	}
}
