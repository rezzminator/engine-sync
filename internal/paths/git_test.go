package paths

import (
	"strings"
	"testing"
)

func TestGitFacadeReturnsSubprocessOutput(t *testing.T) {
	raw, err := Git("--version")
	if err != nil || !strings.HasPrefix(string(raw), "git version") {
		t.Fatalf("%q %v", raw, err)
	}
}
