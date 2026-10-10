package cliargs

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testjail.RunTests(m)) }
func TestProjectExplicitEmptyOverrides(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	var out bytes.Buffer
	o, _, rc, ok := ParseProjectOptions("build", []string{"--root-adapter", "", "--agent-preamble", "", "--model", "large=mapped"}, &out, &out, func([]string) (string, error) { return root, nil }, func() (string, error) { return home, nil })
	if !ok || rc != 0 || !o.CLIOverrides.SetRootAdapter || !o.CLIOverrides.SetAgentPreamble || o.CLIOverrides.ModelMap["large"] != "mapped" {
		t.Fatalf("%#v %d %v %s", o, rc, ok, &out)
	}
}
