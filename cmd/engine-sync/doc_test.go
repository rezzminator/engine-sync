package main

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/cli"
	"github.com/rezzminator/engine-sync/internal/guard"
	"strings"
	"testing"
)

func TestReleaseMetadataReachesVersionCommand(t *testing.T) {
	var out, err bytes.Buffer
	rc := cli.Main([]string{"version"}, &out, &err, strings.NewReader(""), cli.Version)
	if !strings.Contains(cli.Version, "-") || !guard.Newer(cli.Version, "0.1.0") {
		t.Errorf("development version %q must be a pre-release newer than 0.1.0", cli.Version)
	}
	if rc != 0 || out.String() != "engine-sync "+cli.Version+"\n" {
		t.Fatalf("%d %q", rc, out.String())
	}
}
