package pluginlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rezzminator/engine-sync/internal/atomicfile"
	"github.com/rezzminator/engine-sync/internal/paths"
)

const CapBytes = 20971520
const RowMaxBytes = 4096

// Row contains only operational metadata, never prompt, transcript or tool bodies.
type Row struct {
	Level   string `json:"level"`
	Event   string `json:"event"`
	Msg     string `json:"msg"`
	Where   string `json:"where"`
	Session string `json:"session,omitempty"`
	Detail  any    `json:"detail,omitempty"`
}

var processLog struct {
	sync.Mutex
	failing bool
}

// Record is the process-wide append facade, with one fallback per failing streak.
// get is the command's environment; nil uses the paths facade's process reader.
func Record(get paths.Env, stderr io.Writer, row Row) {
	processLog.Lock()
	defer processLog.Unlock()
	if get == nil {
		get = paths.Getenv
	}
	home := get("HOME")
	var err error
	if home == "" && get("PLUGIN_LOG_DIR") == "" {
		home, err = paths.Home()
	}
	dir := paths.PluginLogDir(get, home)
	if err == nil {
		err = appendRow(dir, row)
	}
	if err != nil {
		if !processLog.failing {
			if stderr == nil {
				stderr = os.Stderr
			}
			fmt.Fprintf(stderr, "engine-sync: plugin log %s: %v\n", dir, err)
		}
		processLog.failing = true
		return
	}
	processLog.failing = false
}

type wireRow struct {
	TS     string `json:"ts"`
	Plugin string `json:"plugin"`
	Row
}

func newWireRow(row Row) wireRow {
	return wireRow{TS: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), Plugin: "engine-sync", Row: row}
}

func encodeRow(row Row) ([]byte, error) {
	wire := newWireRow(row)
	raw, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", row.Event, err)
	}
	if len(raw)+1 > RowMaxBytes {
		wire.Detail = map[string]any{"truncated": true, "bytes": len(raw) + 1}
		runes := []rune(row.Msg)
		low, high := 0, len(runes)
		for low < high {
			middle := (low + high + 1) / 2
			wire.Msg = string(runes[:middle])
			candidate, marshalErr := json.Marshal(wire)
			if marshalErr != nil {
				return nil, fmt.Errorf("encode truncated %s: %w", row.Event, marshalErr)
			}
			if len(candidate)+1 <= RowMaxBytes {
				low = middle
			} else {
				high = middle - 1
			}
		}
		wire.Msg = string(runes[:low])
		raw, err = json.Marshal(wire)
		if err != nil {
			return nil, fmt.Errorf("encode truncated %s: %w", row.Event, err)
		}
		if len(raw)+1 > RowMaxBytes {
			return nil, fmt.Errorf("encode %s: metadata exceeds %d bytes", row.Event, RowMaxBytes)
		}
	}
	return append(raw, '\n'), nil
}

func appendRow(dir string, row Row) error {
	files := map[string]string{"error": "errors.jsonl", "warning": "warnings.jsonl", "info": "info.jsonl"}
	name, ok := files[row.Level]
	if !ok {
		return fmt.Errorf("event %s: invalid level %q", row.Event, row.Level)
	}
	raw, err := encodeRow(row)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	path := filepath.Join(dir, name)
	capped := false
	if info, err := os.Stat(path); err == nil {
		if info.Size() > CapBytes {
			if err := atomicfile.MoveAside(path, path+".1"); err != nil {
				return err
			}
			capped = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("append %s: %w", path, err)
	}
	n, writeErr := file.Write(raw)
	if writeErr == nil && n != len(raw) {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("append %s: %w", path, err)
	}
	if capped {
		return appendRow(dir, Row{Level: "warning", Event: "log-capped", Session: row.Session, Msg: name + " reached the cap; previous rows moved to " + name + ".1", Where: "internal/pluginlog/pluginlog.go#appendRow", Detail: map[string]any{"file": name, "cap_bytes": CapBytes}})
	}
	return nil
}

// ProjectOutcome is the diagnostic summary of one project command, not hook output.
type ProjectOutcome struct {
	Action, Session    string
	Error              error
	Code               int
	Applied            bool
	Warnings, Problems []string
}

// buildWarningRow names the warnings in detail.warnings, bounded to RowMaxBytes:
// whole warnings first, the next one cut to the room left, the rest counted in
// detail.omitted, with detail.truncated (and detail.cut for a cut text) set.
func buildWarningRow(outcome ProjectOutcome, where string) Row {
	row := Row{Level: "warning", Event: "build-warning", Msg: fmt.Sprintf("%s reported %d warnings", outcome.Action, len(outcome.Warnings)), Where: where, Session: outcome.Session}
	count, warnings := len(outcome.Warnings), outcome.Warnings
	wire := newWireRow(row)
	fits := func(detail map[string]any) bool {
		wire.Detail = detail
		raw, err := json.Marshal(wire)
		return err == nil && len(raw)+1 <= RowMaxBytes
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
		// Not even the count fits: the generic path in encodeRow marks the row truncated.
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
func OutcomeRows(outcome ProjectOutcome) []Row {
	var rows []Row
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
	if outcome.Code != 0 {
		level, event, msg = "error", outcome.Action+"-failed", outcome.Action+" failed"
		if outcome.Error != nil {
			msg += ": " + outcome.Error.Error()
		}
		if len(outcome.Problems) > 0 {
			msg += ": " + strings.Join(outcome.Problems, "; ")
		}
	}
	return append(rows, Row{Level: level, Event: event, Msg: msg, Where: where, Session: outcome.Session, Detail: map[string]any{"code": outcome.Code, "problems": len(outcome.Problems)}})
}

// RecordOutcome keeps the CLI orchestrator independent of row formatting.
func RecordOutcome(get paths.Env, stderr io.Writer, outcome ProjectOutcome) {
	for _, row := range OutcomeRows(outcome) {
		Record(get, stderr, row)
	}
}
