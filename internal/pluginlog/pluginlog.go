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

func encodeRow(row Row) ([]byte, error) {
	wire := struct {
		TS     string `json:"ts"`
		Plugin string `json:"plugin"`
		Row
	}{TS: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), Plugin: "engine-sync", Row: row}
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
	if info, err := os.Lstat(dir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory %s is a symlink", dir)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect directory %s: %w", dir, err)
	}
	for _, level := range []string{"errors.jsonl", "warnings.jsonl", "info.jsonl"} {
		for _, suffix := range []string{"", ".1"} {
			leaf := filepath.Join(dir, level+suffix)
			if info, err := os.Lstat(leaf); err == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("endpoint %s is a symlink", leaf)
				}
				if !info.Mode().IsRegular() {
					return fmt.Errorf("endpoint %s is not a regular file", leaf)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect %s: %w", leaf, err)
			}
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("set directory mode: %w", err)
	}
	for _, level := range []string{"errors.jsonl", "warnings.jsonl", "info.jsonl"} {
		for _, suffix := range []string{"", ".1"} {
			leaf := filepath.Join(dir, level+suffix)
			if _, err := os.Lstat(leaf); err == nil {
				if err := os.Chmod(leaf, 0600); err != nil {
					return fmt.Errorf("set mode %s: %w", leaf, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect %s: %w", leaf, err)
			}
		}
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
	if err := file.Chmod(0600); err != nil {
		return fmt.Errorf("set mode %s: %w", path, errors.Join(err, file.Close()))
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

// OutcomeRows summarizes warnings once and records only an actual build or its failure.
func OutcomeRows(outcome ProjectOutcome) []Row {
	var rows []Row
	if outcome.Error != nil {
		outcome.Code = 1
	}
	where := "internal/cli/build.go#runProjectTargets"
	if len(outcome.Warnings) > 0 {
		rows = append(rows, Row{Level: "warning", Event: "build-warning", Msg: fmt.Sprintf("%s reported %d warnings", outcome.Action, len(outcome.Warnings)), Where: where, Session: outcome.Session, Detail: map[string]any{"count": len(outcome.Warnings)}})
	}
	if outcome.Action != "build" || outcome.Code == 2 || (outcome.Code == 0 && !outcome.Applied) {
		return rows
	}
	level, event, msg := "info", "build-done", "build completed"
	if outcome.Code != 0 {
		level, event, msg = "error", "build-failed", "build failed"
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
