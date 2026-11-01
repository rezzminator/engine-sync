package machine

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/artifact"
)

func TestMachineDoctorRows(t *testing.T) {
	home, repo := fixture(t)
	cases := []struct {
		name    string
		options DoctorOptions
		stage   func(t *testing.T)
		want    []Row
	}{
		{
			name:    "no clone is named, not warned",
			options: DoctorOptions{Home: home},
			want:    []Row{{LevelWarn, "global-agents source=" + filepath.Join(home, ".claude", "agents") + ` state=NO-SOURCES hint="run engine-sync global"`}},
		},
		{
			name:    "claude absent names the store",
			options: DoctorOptions{Home: home, SourceRepo: repo, ClaudeAbsent: true},
			want: []Row{{LevelOK, "global-agents store dir=" + filepath.Join(home, ".claude") + " state=NO-CLAUDE" +
				` note="no Claude Code binary installed — the installer never wires the store's agents"`}},
		},
		{
			name:    "nothing installed fails both registries",
			options: DoctorOptions{Home: home, SourceRepo: repo},
			want: []Row{
				{LevelFail, "global-agents store dir=" + filepath.Join(home, ".claude") + ` state=MISSING names=gamma,gamma-deep hint="run engine-sync global"`},
				{LevelFail, "global-agents source=" + filepath.Join(home, ".codex", "agents") + ` state=MISSING names=gamma,gamma-deep hint="run engine-sync global"`},
			},
		},
		{
			name:    "an applied install is clean",
			options: DoctorOptions{Home: home, SourceRepo: repo},
			stage: func(t *testing.T) {
				if report := RunMachine(Options{Home: home, SourceRepo: repo}); report.Err != nil {
					t.Fatal(report.Err)
				}
			},
			want: []Row{
				{LevelOK, "global-agents store dir=" + filepath.Join(home, ".claude") + " state=linked"},
				{LevelOK, "global-agents source=" + filepath.Join(home, ".codex", "agents") + " state=installed"},
			},
		},
		{
			name:    "a drifted role is a mismatch",
			options: DoctorOptions{Home: home, SourceRepo: repo},
			stage: func(t *testing.T) {
				writeMachineFixture(t, filepath.Join(home, ".codex", "agents", "gamma.toml"), generatedRole)
			},
			want: []Row{
				{LevelOK, "global-agents store dir=" + filepath.Join(home, ".claude") + " state=linked"},
				{LevelFail, "global-agents source=" + filepath.Join(home, ".codex", "agents") + ` state=MISMATCH names=gamma hint="run engine-sync global"`},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.stage != nil {
				tc.stage(t)
			}
			got := []Row{}
			for _, status := range InspectRegistries(tc.options) {
				got = append(got, Row{status.Level(), "global-agents " + status.Describe()})
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("rows\n got %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

func TestMachineDoctorReportsDanglingSources(t *testing.T) {
	home, repo := fixture(t)
	path := filepath.Join(repo, "agents", "broken.md")
	missing := filepath.Join(repo, "missing.md")
	if err := os.Symlink(missing, path); err != nil {
		t.Fatal(err)
	}
	// The dangling source is one warn row; every registry is still inspected.
	want := []Row{
		{LevelWarn, "global-agents source=" + filepath.Join(repo, "agents") + " state=WARNING error=dangling source " + path + ": lstat " + missing + `: no such file or directory hint="restore or remove the named source"`},
		{LevelFail, "global-agents store dir=" + filepath.Join(home, ".claude") + ` state=MISSING names=gamma,gamma-deep hint="run engine-sync global"`},
		{LevelFail, "global-agents source=" + filepath.Join(home, ".codex", "agents") + ` state=MISSING names=gamma,gamma-deep hint="run engine-sync global"`},
	}
	got := []Row{}
	for _, status := range InspectRegistries(DoctorOptions{Home: home, SourceRepo: repo}) {
		got = append(got, Row{status.Level(), "global-agents " + status.Describe()})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows\n got %#v\nwant %#v", got, want)
	}
	rows := Doctor(DoctorOptions{Home: home, SourceRepo: repo})
	if !slices.Contains(rows, want[0]) {
		t.Fatalf("doctor omitted the dangling source row %q: %+v", want[0].Message, rows)
	}
}

// TestMachineDoctorStaleOnceAndNoScanWithoutConfig: a STALE agent is one
// row (its plan retire event is not a second), and a config that fails to
// load is reported with the agent scan named NOT SCANNED, never as clean,
// and no directory scanned from a config that never loaded.
func TestMachineDoctorStaleOnceAndNoScanWithoutConfig(t *testing.T) {
	home, repo := fixture(t)
	if report := RunMachine(Options{Home: home, SourceRepo: repo}); report.Err != nil {
		t.Fatal(report.Err)
	}
	stale := filepath.Join(home, ".codex", "agents", "old.toml")
	writeMachineFixture(t, stale, artifact.Marker(artifact.TOML, "agents/old.md")+"\nname = \"old\"\n")
	rows := Doctor(DoctorOptions{Home: home, SourceRepo: repo})
	count := 0
	for _, row := range rows {
		if strings.Contains(row.Message, stale) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("rows naming %s = %d, want 1: %+v", stale, count, rows)
	}
	policy := filepath.Join(home, ".claude", "engine-sync.json")
	writeMachineFixture(t, policy, `{"version":1,"machine":`)
	var text []string
	for _, row := range Doctor(DoctorOptions{Home: home, SourceRepo: repo}) {
		text = append(text, row.Level+" "+row.Message)
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{LevelFail + " global-agents states: parse config " + policy + ": EOF", LevelFail + " global-agents states NOT SCANNED"} {
		if !strings.Contains(all, want) {
			t.Fatalf("doctor lacks %q:\n%s", want, all)
		}
	}
	for _, lack := range []string{stale, "\n" + LevelFail + " agent "} {
		if strings.Contains(all, lack) {
			t.Fatalf("doctor scanned without a config (%q):\n%s", lack, all)
		}
	}
}

// TestMachineDoctorInspectsHealthyAgentsBesideABrokenOne: a source breaking
// the agent contract is a BROKEN row naming it, never the whole directory
// UNREADABLE, and every healthy agent's Codex registry is still inspected —
// global skips the broken agent and installs the rest.
func TestMachineDoctorInspectsHealthyAgentsBesideABrokenOne(t *testing.T) {
	home, repo := fixture(t)
	path := filepath.Join(repo, "agents", "broken.md")
	plantFile(t, path, []byte("---\nname: broken\n---\n\nno description field.\n"))
	statuses := InspectRegistries(DoctorOptions{Home: home, SourceRepo: repo})
	var broken, codex bool
	for _, s := range statuses {
		if s.State == AgentsUnreadable && s.Dir == filepath.Join(repo, "agents") {
			t.Fatalf("a readable agents directory reported UNREADABLE: %+v", statuses)
		}
		broken = broken || (s.State == AgentsBroken && strings.Join(s.Names, ",") == "broken" && strings.Contains(s.Error, path))
		codex = codex || (!s.Store && s.Dir == filepath.Join(home, ".codex", "agents"))
	}
	if !broken || !codex {
		t.Fatalf("statuses = %+v, want a BROKEN row naming broken and the Codex registry inspected", statuses)
	}
}
