package machine

import (
	"reflect"
	"testing"
)

func TestPlanBlockersPreservesFailures(t *testing.T) {
	got := planBlockers([]string{"MISSING x", "STALE y", "ORPHAN z", "CONFLICT user", "read source: failed"})
	want := []string{"CONFLICT user", "read source: failed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}
