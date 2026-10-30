// Package runlog is engine-sync's adapter onto the vendored plugin-log library: where its rows go and which rows a
// project command writes.
package runlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rezzminator/engine-sync/internal/guard"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/pluginlog"
)

// Record writes row through the process's engine-sync Logger. get is the command's environment; nil uses the paths
// facade's process reader. It prints no row line: callers print their own error lines, and the library says a failing
// streak once on stderr.
func Record(get paths.Env, stderr io.Writer, row pluginlog.Row) {
	if get == nil {
		get = paths.Getenv
	}
	home := get("HOME")
	if home == "" && get("PLUGIN_LOG_DIR") == "" {
		// On error home stays "": the library reports "plugin log off".
		if h, err := paths.Home(); err == nil {
			home = h
		}
	}
	pluginlog.For("engine-sync", pluginlog.Dir(get("PLUGIN_LOG_DIR"), home, "engine-sync"), stderr).Record(row)
}

// ProjectOutcome is the diagnostic summary of one project command, not hook output.
type ProjectOutcome struct {
	Action, Session    string
	Error              error
	Code               int
	Applied            bool
	Warnings, Problems []string
}

// buildWarningRow names the warnings in detail.warnings, bounded to pluginlog.RowMaxBytes:
// whole warnings first, the next one cut to the room left, the rest counted in
// detail.omitted, with detail.truncated (and detail.cut for a cut text) set.
func buildWarningRow(outcome ProjectOutcome, where string) pluginlog.Row {
	row := pluginlog.Row{Level: "warning", Event: "build-warning", Msg: fmt.Sprintf("%s reported %d warnings", outcome.Action, len(outcome.Warnings)), Where: where, Session: outcome.Session}
	count, warnings := len(outcome.Warnings), outcome.Warnings
	// json.Marshal escapes HTML, so it is never shorter than pluginlog.Encode: the measure is conservative.
	wire := pluginlog.Row{TS: "2006-01-02T15:04:05.000Z", Plugin: "engine-sync", Level: row.Level, Event: row.Event, Msg: row.Msg, Where: row.Where, Session: row.Session}
	fits := func(detail map[string]any) bool {
		wire.Detail = detail
		raw, err := json.Marshal(wire)
		return err == nil && len(raw)+1 <= pluginlog.RowMaxBytes
	}
	whole := map[string]any{"count": count, "warnings": warnings}
	if fits(whole) {
		row.Detail = whole
		return row
	}
	partial := func(shown []string, omitted int, cut bool) map[string]any {
		detail := map[string]any{"count": count, "warnings": shown, "truncated": true, "omitted": omitted}
		if cut {
			detail["cut"] = true
		}
		return detail
	}
	// The most whole warnings that fit; fits is monotonic in the prefix length.
	low, high := 0, count
	for low < high {
		middle := (low + high + 1) / 2
		if fits(partial(warnings[:middle], count-middle, false)) {
			low = middle
		} else {
			high = middle - 1
		}
	}
	shown := append(make([]string, 0, low+1), warnings[:low]...)
	best := partial(shown, count-low, false)
	if !fits(best) {
		// Not even an empty list fits (an oversized session or location): keep
		// the most that does, always saying the texts were withheld.
		for _, detail := range []map[string]any{
			{"count": count, "truncated": true, "omitted": count},
			{"count": count, "truncated": true},
			{"truncated": true},
			{"count": count},
		} {
			if fits(detail) {
				row.Detail = detail
				return row
			}
		}
		// Not even the count fits: pluginlog.Encode marks the row truncated.
		row.Detail = map[string]any{"count": count}
		return row
	}
	withCut := func(whole []string, text []rune) []string {
		return append(append(make([]string, 0, len(whole)+1), whole...), string(text))
	}
	if low < count {
		runes := []rune(warnings[low])
		lo, hi := 0, len(runes)
		for lo < hi {
			middle := (lo + hi + 1) / 2
			if fits(partial(withCut(shown, runes[:middle]), count-low-1, true)) {
				lo = middle
			} else {
				hi = middle - 1
			}
		}
		if lo > 0 {
			best = partial(withCut(shown, runes[:lo]), count-low-1, true)
		}
	}
	row.Detail = best
	return row
}

// OutcomeRows summarizes warnings once and records builds and failed project checks.
func OutcomeRows(outcome ProjectOutcome) []pluginlog.Row {
	var rows []pluginlog.Row
	if outcome.Error != nil {
		outcome.Code = 1
	}
	where := "internal/cli/build.go#runProjectTargets"
	if len(outcome.Warnings) > 0 {
		rows = append(rows, buildWarningRow(outcome, where))
	}
	if outcome.Code == 2 || (outcome.Code == 0 && (outcome.Action != "build" || !outcome.Applied)) {
		return rows
	}
	if outcome.Action != "build" && outcome.Action != "check" && outcome.Action != "doctor" {
		return rows
	}
	level, event, msg := "info", "build-done", "build completed"
	if errors.Is(outcome.Error, guard.ErrBusy) {
		// A live holder is no fault: the run repeats once it lets go.
		level, event, msg = "info", "lock-busy", outcome.Action+" refused: "+outcome.Error.Error()
	} else if outcome.Code != 0 {
		level, event, msg = "error", outcome.Action+"-failed", outcome.Action+" failed"
		if outcome.Error != nil {
			msg += ": " + outcome.Error.Error()
		}
		if len(outcome.Problems) > 0 {
			msg += ": " + strings.Join(outcome.Problems, "; ")
		}
	}
	return append(rows, pluginlog.Row{Level: level, Event: event, Msg: msg, Where: where, Session: outcome.Session, Detail: map[string]any{"code": outcome.Code, "problems": len(outcome.Problems)}})
}

// RecordOutcome keeps the CLI orchestrator independent of row formatting.
func RecordOutcome(get paths.Env, stderr io.Writer, outcome ProjectOutcome) {
	for _, row := range OutcomeRows(outcome) {
		Record(get, stderr, row)
	}
}
