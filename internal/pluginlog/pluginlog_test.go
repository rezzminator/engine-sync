package pluginlog_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rezzminator/engine-sync/internal/pluginlog"
	"github.com/rezzminator/engine-sync/internal/testjail"
)

func fixedNow() time.Time { return time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC) }

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func TestDirResolvesOverrideThenHomeThenOff(t *testing.T) {
	for _, c := range []struct{ override, home, want string }{
		{"/logs/", "/home/u", "/logs/p"},
		{"", "/home/u/", "/home/u/.local/state/p/logs"},
		{"", "", ""},
	} {
		if got := pluginlog.Dir(c.override, c.home, "p"); got != c.want {
			t.Errorf("Dir(%q, %q) = %q, want %q", c.override, c.home, got, c.want)
		}
	}
}

func TestEncodeBoundsAnOversizedRowAndRefusesAnUnknownLevel(t *testing.T) {
	row := pluginlog.Row{TS: "t", Plugin: "p", Level: "info", Event: "e", Msg: "m", Where: "w", Detail: strings.Repeat("x", 5000)}
	raw, err := pluginlog.Encode(row)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"ts":"t","plugin":"p","level":"info","event":"e","msg":"m","where":"w","detail":{"truncated":true,"bytes":5085}}` + "\n"
	if string(raw) != want {
		t.Errorf("Encode oversized = %q, want %q", raw, want)
	}
	row.Level = "loud"
	if _, err := pluginlog.Encode(row); err == nil || err.Error() != `plugin log invalid level: "loud"` {
		t.Errorf("Encode unknown level error = %v", err)
	}
}

func TestRecordAppendsRotatesAndHoldsRowsAcrossAFailure(t *testing.T) {
	dir := filepath.Join(testjail.Scratch(t), "logs")
	var screen bytes.Buffer
	logger := pluginlog.New("p", dir, &screen, fixedNow)
	logger.Record(pluginlog.Row{Level: "info", Event: "e", Msg: "m", Where: "w"})
	want := `{"ts":"2026-11-01T05:00:00.000Z","plugin":"p","level":"info","event":"e","msg":"m","where":"w"}` + "\n"
	if got := read(t, filepath.Join(dir, "info.jsonl")); got != want {
		t.Errorf("info.jsonl = %q, want %q", got, want)
	}

	// A full level file moves to .1 and a log-capped warning says so.
	info := filepath.Join(dir, "info.jsonl")
	if err := os.Truncate(info, pluginlog.CapBytes+1); err != nil {
		t.Fatal(err)
	}
	logger.Record(pluginlog.Row{Level: "info", Event: "e2", Msg: "m", Where: "w"})
	if st, err := os.Stat(info + ".1"); err != nil || st.Size() != pluginlog.CapBytes+1 {
		t.Errorf("info.jsonl.1 = %v, %v, want size %d", st, err, pluginlog.CapBytes+1)
	}
	if got := read(t, info); !strings.Contains(got, `"event":"e2"`) || strings.Count(got, "\n") != 1 {
		t.Errorf("fresh info.jsonl = %q, want the one e2 row", got)
	}
	if got := read(t, filepath.Join(dir, "warnings.jsonl")); !strings.Contains(got, `"event":"log-capped"`) || !strings.Contains(got, `"file":"info.jsonl"`) {
		t.Errorf("warnings.jsonl = %q, want a log-capped row for info.jsonl", got)
	}

	// An unwritable directory holds the row, says one screen line, and the next success writes it first.
	bad := filepath.Join(testjail.Scratch(t), "file")
	if err := os.WriteFile(bad, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	screen.Reset()
	failing := pluginlog.New("p", filepath.Join(bad, "logs"), &screen, fixedNow)
	failing.Record(pluginlog.Row{Level: "info", Event: "a", Msg: "m", Where: "w"})
	failing.Record(pluginlog.Row{Level: "info", Event: "b", Msg: "m", Where: "w"})
	if failing.Held() != 2 || strings.Count(screen.String(), "\n") != 1 || !strings.HasPrefix(screen.String(), "p: plugin log "+bad) {
		t.Errorf("held %d, screen %q, want 2 held rows and one screen line", failing.Held(), screen.String())
	}
}
