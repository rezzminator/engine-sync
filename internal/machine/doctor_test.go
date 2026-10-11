package machine

import (
	"path/filepath"
	"reflect"
	"testing"
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
