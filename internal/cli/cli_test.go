package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandExitCodes(t *testing.T) {
	_, e := project(t)
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"version"}, 0},
		{[]string{}, 2},
		{[]string{"bogus"}, 2},
		{[]string{"codex", "bogus"}, 2},
		{[]string{"opencode", "bogus"}, 2},
		{[]string{"hook", "claude"}, 2},
		{[]string{"hook", "codex", "inject"}, 0},
	} {
		var stdout, stderr bytes.Buffer
		if got := dispatch(tc.args, &stdout, &stderr, e); got != tc.want {
			t.Errorf("run %q = %d, want %d (stderr %q)", tc.args, got, tc.want, stderr.String())
		}
	}
}

func TestMachineUsageNamesGptOptIn(t *testing.T) {
	_, e := project(t)
	for _, args := range [][]string{{"global", "--help"}, {"codex", "agents", "--help"}, {"doctor", "--machine", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			dispatch(args, &stdout, &stderr, e)
			first, _, _ := strings.Cut(stderr.String(), "\n")
			if !strings.Contains(first, "--gpt-agents") {
				t.Fatalf("usage omits machine opt-in: %q", first)
			}
		})
	}
}
