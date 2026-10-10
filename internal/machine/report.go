package machine

import (
	"encoding/json"
	"fmt"
	"io"
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
