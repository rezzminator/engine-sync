package machine

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/target/codex"
)

func PrintReport(report Report, asJSON bool, stdout, stderr io.Writer) int {
	ok := report.OK && report.Err == nil
	errText := ""
	if report.Err != nil {
		errText = report.Err.Error()
	}
	if asJSON {
		events := report.Events
		if events == nil {
			events = []Event{}
		}
		if err := json.NewEncoder(stdout).Encode(struct {
			OK     bool    `json:"ok"`
			Events []Event `json:"events"`
			Error  string  `json:"error"`
		}{OK: ok, Events: events, Error: errText}); err != nil {
			fmt.Fprintf(stderr, "engine-sync global: write JSON: %v\n", err)
			return 1
		}
	} else {
		for _, event := range report.Events {
			verb := event.Verb
			if verb == VerbRetire {
				verb = VerbChange
			}
			fmt.Fprintf(stdout, "  %-7s %s\n", verb, event.Message)
		}
		if errText != "" {
			fmt.Fprintf(stderr, "engine-sync global: %s\n", errText)
		}
	}
	if !ok {
		return 1
	}
	return 0
}

// PrintDoctor writes the machine doctor's rows, preceded by the config
// provenance, as text or as one JSON object, and returns the exit code: 1 when
// err is set or any row fails.
func PrintDoctor(rows []Row, sources []config.SourceRow, err error, asJSON bool, stdout, stderr io.Writer) int {
	failed := err != nil
	for _, row := range rows {
		failed = failed || row.Level == LevelFail
	}
	if asJSON {
		value := struct {
			OK            bool               `json:"ok"`
			Rows          []Row              `json:"rows"`
			ConfigSources []config.SourceRow `json:"configSources"`
			Error         string             `json:"error,omitempty"`
		}{!failed, rows, sources, ""}
		if err != nil {
			value.Rows, value.ConfigSources, value.Error = []Row{}, []config.SourceRow{}, err.Error()
		}
		if writeErr := json.NewEncoder(stdout).Encode(value); writeErr != nil {
			fmt.Fprintf(stderr, "engine-sync doctor: write JSON: %v\n", writeErr)
			return 1
		}
	} else if err != nil {
		fmt.Fprintln(stderr, err)
	} else {
		for _, row := range sources {
			fmt.Fprintf(stdout, "CONFIG %s=%s (%s)\n", row.Key, row.Value, row.Source)
		}
		for _, row := range rows {
			fmt.Fprintf(stdout, "%s %s\n", row.Level, row.Message)
		}
	}
	if failed {
		return 1
	}
	return 0
}

// PrintGlobalAgents writes one `codex agents` result: warnings and problems
// on stderr, every compiled role, link and role state on stdout, then
// CODEX AGENTS PASS when nothing failed. It returns the exit code.
func PrintGlobalAgents(result GlobalAgentsResult, stdout, stderr io.Writer) int {
	for _, warning := range result.Warnings {
		fmt.Fprintln(stderr, "engine-sync codex agents: warning: "+warning)
	}
	for _, compiled := range result.Compiled {
		fmt.Fprintf(stdout, "compiled %s: %d B, parses clean\n", compiled.Path, compiled.Size)
	}
	for _, installed := range result.Installed {
		fmt.Fprintf(stdout, "%s %s\n", installed.State, codex.DescribeGlobalLinkState(installed.State, installed.Path, installed.Source, installed.Found))
	}
	for _, role := range result.Roles {
		fmt.Fprintf(stdout, "%s %s\n", role.State, role.Describe())
	}
	for _, problem := range result.Problems {
		fmt.Fprintf(stderr, "engine-sync codex agents: %s\n", problem)
	}
	if len(result.Problems) > 0 {
		return 1
	}
	fmt.Fprintln(stdout, "CODEX AGENTS PASS")
	return 0
}
