package diag

import (
	"testing"

	"github.com/rezzminator/engine-sync/internal/source"
)

func TestPluginFailedRefusesOnlyTheKeptOutput(t *testing.T) {
	got := PluginFailed(source.PluginFailure{Plugin: "flights", Problem: "plugin flights: could not resolve flights@market in /cfg: parse /cfg/plugins/known_marketplaces.json: unexpected EOF"}, "/codex/agents/flights-")
	want := Diagnostic{Severity: Error, Path: "/codex/agents/flights-", Message: "plugin flights: could not resolve flights@market in /cfg: parse /cfg/plugins/known_marketplaces.json: unexpected EOF; its twins are kept"}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}
