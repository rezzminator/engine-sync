package diag

import (
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"io"
)

// JSONResult is the --json shape every engine-sync compile command prints
// (docs/interface.md): rebuildable is the multiset of problems a build would
// repair, error a run failure.
type JSONResult struct {
	ConfigSources []config.SourceRow `json:"configSources,omitempty"`
	OK            bool               `json:"ok"`
	Problems      []string           `json:"problems"`
	Rebuildable   []string           `json:"rebuildable"`
	Warnings      []string           `json:"warnings"`
	Wrote         int                `json:"wrote"`
	Unchanged     int                `json:"unchanged"`
	Deleted       int                `json:"deleted"`
	Error         string             `json:"error"`
}

// WriteJSON prints one result as JSON and returns its exit code: 0 when it
// passed, 1 on a finding or a run failure.
func WriteJSON(stdout, stderr io.Writer, result Report, runErr error, sources ...[]config.SourceRow) int {
	out := JSONResult{
		OK: result.OK && runErr == nil, Problems: nonNil(result.Problems), Rebuildable: nonNil(result.Rebuildable),
		Warnings: nonNil(result.Warnings), Wrote: result.Wrote, Unchanged: result.Unchanged, Deleted: result.Deleted,
	}
	if len(sources) > 0 {
		out.ConfigSources = sources[0]
	}
	if runErr != nil {
		out.Error = runErr.Error()
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		fmt.Fprintf(stderr, "engine-sync: write JSON result: %v\n", err)
		return 1
	}
	if !out.OK {
		return 1
	}
	return 0
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
