package source

import "testing"

// Each section path keeps its own new pin, in rewrite order.
func TestByPathPinsKeepsEachPathsPin(t *testing.T) {
	got := byPathPins([]string{"a", "b"}, func(key string) string { return "pin-" + key })
	if len(got) != 2 || got["a"] != "pin-a" || got["b"] != "pin-b" {
		t.Fatalf("byPathPins = %v", got)
	}
}
