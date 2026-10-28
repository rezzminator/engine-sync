package runlog_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rezzminator/engine-sync/internal/cli"
	"github.com/rezzminator/engine-sync/internal/cliargs"
	"github.com/rezzminator/engine-sync/internal/runlog"
	"github.com/rezzminator/engine-sync/internal/testjail"
)

func pluginLogRows(t *testing.T, dir, level string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "engine-sync", level+".jsonl"))
	if err != nil {
		t.Fatalf("plugin log row missing: %v", err)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Fatalf("not newline terminated: %q", raw)
	}
	var rows []map[string]any
	for _, line := range bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n")) {
		if len(line)+1 > 4096 {
			t.Fatalf("row exceeds byte bound: %d", len(line)+1)
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("invalid row: %v", err)
		}
		if row["plugin"] != "engine-sync" {
			t.Fatalf("wrong plugin: %v", row)
		}
		if _, err := time.Parse(time.RFC3339Nano, row["ts"].(string)); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestPluginLogContract(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		fallback      bool
	}{
		{"explicit", "fixture failure", false},
		{"fallback", "fixture failure", true},
		{"largest", strings.Repeat("界\\\"\n", 5000), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testjail.Scratch(t)
			dir := filepath.Join(root, "logs")
			t.Setenv("PLUGIN_LOG_DIR", dir)
			if tc.fallback {
				t.Setenv("PLUGIN_LOG_DIR", "")
				t.Setenv("HOME", root)
				dir = filepath.Join(root, ".local", "state")
			}
			var out, stderr bytes.Buffer
			code := cliargs.ReportFailure("fixture", false, errors.New(tc.message), &out, &stderr)
			if code != 1 || !strings.Contains(stderr.String(), tc.message) {
				t.Fatalf("work result changed: %d %q", code, stderr.String())
			}
			// Fallback does not have the shared-directory engine-sync suffix above logs.
			readDir := dir
			if tc.fallback {
				readDir = filepath.Join(root, ".local", "state", "engine-sync", "logs")
				raw, err := os.ReadFile(filepath.Join(readDir, "errors.jsonl"))
				if err != nil {
					t.Fatalf("fallback row missing: %v", err)
				}
				var row map[string]any
				if err := json.Unmarshal(bytes.TrimSpace(raw), &row); err != nil {
					t.Fatal(err)
				}
				if row["event"] != "run-failed" || row["msg"] != "fixture: fixture failure" {
					t.Fatalf("fallback row: %v", row)
				}
				return
			}
			rows := pluginLogRows(t, readDir, "errors")
			if len(rows) != 1 || rows[0]["level"] != "error" || rows[0]["event"] != "run-failed" || rows[0]["where"] != "internal/cliargs/project.go#ReportFailure" {
				t.Fatalf("wrong error row: %v", rows)
			}
			if tc.name == "largest" {
				detail := rows[0]["detail"].(map[string]any)
				if detail["truncated"] != true || detail["bytes"].(float64) <= 4096 {
					t.Fatalf("not truncated: %v", detail)
				}
			} else if rows[0]["msg"] != "fixture: "+tc.message {
				t.Fatalf("wrong msg: %v", rows[0])
			}
			info, err := os.Stat(filepath.Join(dir, "engine-sync", "errors.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("file mode: %o", info.Mode().Perm())
			}
			info, err = os.Stat(filepath.Join(dir, "engine-sync"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0700 {
				t.Fatalf("dir mode: %o", info.Mode().Perm())
			}
		})
	}
}

func TestPluginLogFailureStreak(t *testing.T) {
	root := testjail.Scratch(t)
	dir := filepath.Join(root, "blocked")
	t.Setenv("PLUGIN_LOG_DIR", dir)
	var out, stderr bytes.Buffer
	fail := func() {
		if code := cliargs.ReportFailure("fixture", false, errors.New("work failed"), &out, &stderr); code != 1 {
			t.Fatal(code)
		}
	}
	if err := os.WriteFile(dir, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	fail()
	fail()
	if strings.Count(stderr.String(), "engine-sync: plugin log ") != 1 || !strings.Contains(stderr.String(), dir) || !strings.Contains(stderr.String(), "not a directory") {
		t.Fatalf("fallback streak: %q", stderr.String())
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	fail()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	fail()
	if strings.Count(stderr.String(), "engine-sync: plugin log ") != 2 {
		t.Fatalf("fallback did not rearm: %q", stderr.String())
	}
}

func TestPluginLogUsage(t *testing.T) {
	dir := testjail.Scratch(t)
	t.Setenv("PLUGIN_LOG_DIR", dir)
	for _, args := range [][]string{nil, {"not-a-command"}, {"build", "--not-a-flag"}, {"build", "--engine", "missing"}} {
		var out, stderr bytes.Buffer
		if code := cli.Main(args, &out, &stderr, nil, "0.4.0-alpha"); code != 2 {
			t.Fatalf("%v: code%d", args, code)
		}
	}
	rows := pluginLogRows(t, dir, "info")
	if len(rows) != 4 {
		t.Fatalf("usage rows: %v", rows)
	}
	for _, row := range rows {
		if row["event"] != "usage-refused" {
			t.Fatal(row)
		}
	}
}

func TestPluginLogExplicitDirectoryWithoutHome(t *testing.T) {
	dir := testjail.Scratch(t)
	t.Setenv("PLUGIN_LOG_DIR", dir)
	t.Setenv("HOME", "")
	var out, stderr bytes.Buffer
	if code := cliargs.ReportFailure("fixture", false, errors.New("no home"), &out, &stderr); code != 1 {
		t.Fatal(code)
	}
	rows := pluginLogRows(t, dir, "errors")
	if len(rows) != 1 || rows[0]["event"] != "run-failed" || rows[0]["msg"] != "fixture: no home" {
		t.Fatalf("explicit directory depended on HOME: %v", rows)
	}
}

func warningDetail(t *testing.T, row map[string]any) map[string]any {
	t.Helper()
	detail, ok := row["detail"].(map[string]any)
	if !ok || row["event"] != "build-warning" {
		t.Fatalf("not a build-warning row with detail: %v", row)
	}
	return detail
}

func warningTexts(t *testing.T, detail map[string]any) []string {
	t.Helper()
	list, ok := detail["warnings"].([]any)
	if !ok {
		t.Fatalf("detail.warnings missing: %v", detail)
	}
	texts := make([]string, len(list))
	for i, item := range list {
		texts[i], ok = item.(string)
		if !ok {
			t.Fatalf("warning %d is not text: %v", i, item)
		}
	}
	return texts
}

// recordWarningsFor records one build outcome and returns the warning rows it
// wrote, or nil when the log refused the row (reported on stderr).
func recordWarningsFor(t *testing.T, session string, warnings []string) ([]map[string]any, string) {
	t.Helper()
	dir := testjail.Scratch(t)
	var stderr bytes.Buffer
	runlog.RecordOutcome(func(key string) string {
		if key == "PLUGIN_LOG_DIR" {
			return dir
		}
		return ""
	}, &stderr, runlog.ProjectOutcome{Action: "build", Session: session, Applied: true, Warnings: warnings})
	if _, err := os.Stat(filepath.Join(dir, "engine-sync", "warnings.jsonl")); errors.Is(err, os.ErrNotExist) {
		return nil, stderr.String()
	}
	return pluginLogRows(t, dir, "warnings"), stderr.String()
}

func recordWarnings(t *testing.T, warnings []string) []map[string]any {
	t.Helper()
	rows, stderr := recordWarningsFor(t, "s1", warnings)
	if stderr != "" || rows == nil {
		t.Fatalf("warning row not written: %q", stderr)
	}
	return rows
}

func TestBuildWarningRowNamesWarnings(t *testing.T) {
	want := []string{"agent a: unknown tool \"Foo\"", "界 skill b: description \\ missing\n"}
	rows := recordWarnings(t, want)
	if len(rows) != 1 || rows[0]["msg"] != "build reported 2 warnings" {
		t.Fatalf("wrong rows: %v", rows)
	}
	detail := warningDetail(t, rows[0])
	if detail["count"] != float64(2) || detail["truncated"] != nil || detail["omitted"] != nil {
		t.Fatalf("wrong detail: %v", detail)
	}
	if got := warningTexts(t, detail); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("warning texts: %q", got)
	}
}

func TestBuildWarningRowBoundsOversizedWarnings(t *testing.T) {
	many := make([]string, 50)
	for i := range many {
		many[i] = fmt.Sprintf("warning %02d: ", i) + strings.Repeat("界\"x", 60)
	}
	for _, tc := range []struct {
		name     string
		warnings []string
		omitted  bool
	}{
		{"many", many, true},
		{"one huge", []string{strings.Repeat("界\\\"\n", 5000)}, false},
		{"empty text among huge", []string{"", strings.Repeat("w", 9000)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := recordWarnings(t, tc.warnings) // the helper fails on a row above 4096 bytes
			if len(rows) != 1 {
				t.Fatalf("a long warning set dropped the row: %v", rows)
			}
			detail := warningDetail(t, rows[0])
			if detail["count"] != float64(len(tc.warnings)) || detail["truncated"] != true {
				t.Fatalf("truncation not marked: %v", detail)
			}
			got := warningTexts(t, detail)
			omitted := int(detail["omitted"].(float64))
			if (omitted > 0) != tc.omitted || len(got)+omitted != len(tc.warnings) || len(got) == 0 {
				t.Fatalf("shown %d omitted %d of %d", len(got), omitted, len(tc.warnings))
			}
			for i, text := range got[:len(got)-1] {
				if text != tc.warnings[i] {
					t.Fatalf("warning %d cut before the last one", i)
				}
			}
			last := got[len(got)-1]
			if !strings.HasPrefix(tc.warnings[len(got)-1], last) {
				t.Fatalf("last warning is not a prefix of its source")
			}
			if (last != tc.warnings[len(got)-1]) != (detail["cut"] == true) {
				t.Fatalf("cut flag %v disagrees with the last text", detail["cut"])
			}
		})
	}
}

func TestBuildWarningRowAbsentWithoutWarnings(t *testing.T) {
	rows := runlog.OutcomeRows(runlog.ProjectOutcome{Action: "build", Session: "s1", Applied: true})
	if len(rows) != 1 || rows[0].Event != "build-done" {
		t.Fatalf("a zero-warning build must write only build-done: %v", rows)
	}
}

// A session id (copied from the hook payload, uncapped) that leaves the row
// almost no room: whatever form the detail takes, a row that lost warning
// texts says so, and a warnings list is a list. The one exception is the 7-byte
// window where `{"count":2}` fits and `{"truncated":true}` (7 bytes longer)
// does not; past it pluginlog.Encode's own truncation marks the row.
func TestBuildWarningRowNearlyFullMetadata(t *testing.T) {
	warnings := []string{strings.Repeat("w", 9000), "second"}
	lastWritten, markerless := 0, 0
	for size := 3700; size <= 4000; size++ {
		rows, _ := recordWarningsFor(t, strings.Repeat("s", size), warnings)
		if rows == nil {
			t.Fatalf("session %d bytes: the library shortens an oversized row, never refuses it", size)
		}
		lastWritten = size
		if len(rows) != 1 {
			t.Fatalf("session %d bytes: %v", size, rows)
		}
		detail := warningDetail(t, rows[0])
		if detail["count"] != float64(2) && detail["truncated"] != true {
			t.Fatalf("session %d bytes: neither count nor truncated survived: %v", size, detail)
		}
		if detail["truncated"] != true {
			markerless++
		}
		if list, present := detail["warnings"]; present {
			if _, isList := list.([]any); !isList {
				t.Fatalf("session %d bytes: detail.warnings is %v, not a list", size, list)
			}
		}
	}
	if lastWritten < 3800 {
		t.Fatalf("the sweep ended at %d; the near-full window was not reached", lastWritten)
	}
	if markerless > 7 {
		t.Fatalf("%d session sizes dropped the warnings without detail.truncated; only the 7-byte window may", markerless)
	}
}
