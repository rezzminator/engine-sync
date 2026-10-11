package engine

import (
	"testing"
)

func TestMCPToolNameValidatesTheWholeInput(t *testing.T) {
	for _, input := range []string{"otherXserver__tool", "", "Read", "other__s__t", "mcp__s", "mcp____t"} {
		if _, err := OpenCodeMCPToolName(input); err == nil {
			t.Fatalf("%q accepted", input)
		}
	}
	if got, err := OpenCodeMCPToolName("mcp__a.b__read-file"); err != nil || got != "a_b_read-file" {
		t.Fatalf("%q %v", got, err)
	}
}
