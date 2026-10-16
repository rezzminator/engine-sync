package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestStopRunsTheFourStages ports scripts/test-codex-sync.sh: every Stop with
// the flag set runs codex build, codex check, opencode build and opencode
// check against the project root, through the binary that runs the hook; a
// pass clears the flag silently, a failure blocks (or, on the repair turn,
// warns) naming the failed stage and the binary, and keeps the flag. The
// script's dev-binary freshness cases are gone: the binary is the wrapper's
// pick, $ENGINE_SYNC_BIN first (internal/wrappertest).
func TestStopRunsTheFourStages(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failStage  string
		stopActive bool
		markDuring bool
		wantKey    string
	}{
		{name: "all-pass"},
		// Another session in the same repository edits a Claude source while
		// this Stop's stages run: its edit postdates the build, so the flag
		// must survive this pass for the next Stop to compile it.
		{name: "a mark during the stages", markDuring: true},
		{name: "block", failStage: "codex check", wantKey: "reason"},
		{name: "warning", failStage: "codex check", stopActive: true, wantKey: "systemMessage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, e := project(t)
			flag := setFlag(t, e, root)
			var calls [][]string
			runner := func(stage []string) (int, string) {
				calls = append(calls, stage)
				if tc.markDuring && len(calls) == 2 {
					setFlag(t, e, root)
				}
				if strings.Join(stage[:2], " ") == tc.failStage {
					return 1, "fixture check failure"
				}
				return 0, ""
			}
			var stdout, stderr bytes.Buffer
			payload := hookPayload{StopHookActive: tc.stopActive}
			if code := hookStop(payload, &stdout, &stderr, e, runner); code != 0 {
				t.Fatalf("a Stop hook must never exit nonzero; got %d, stderr %q", code, stderr.String())
			}
			want := [][]string{
				{"codex", "build", root}, {"codex", "check", root},
				{"opencode", "build", root}, {"opencode", "check", root},
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("stages = %q, want %q", calls, want)
			}
			_, flagErr := os.Stat(flag)
			if tc.markDuring {
				if stdout.Len() != 0 || flagErr != nil {
					t.Fatalf("a mark during the stages must keep the flag silently; stdout %q, flag err %v", stdout.String(), flagErr)
				}
				return
			}
			if tc.failStage == "" {
				if stdout.Len() != 0 || flagErr == nil {
					t.Fatalf("a pass must clear the flag silently; stdout %q, flag kept %v", stdout.String(), flagErr == nil)
				}
				return
			}
			var answer map[string]string
			if err := json.Unmarshal(stdout.Bytes(), &answer); err != nil {
				t.Fatalf("answer is not JSON: %v\n%s", err, stdout.String())
			}
			if tc.wantKey == "reason" && answer["decision"] != "block" {
				t.Fatalf("decision = %q, want block", answer["decision"])
			}
			message := answer[tc.wantKey]
			for _, part := range []string{"codex check", writerPath()} {
				if !strings.Contains(message, part) {
					t.Fatalf("%s %q does not name %q", tc.wantKey, message, part)
				}
			}
			if tc.wantKey == "reason" && !strings.Contains(message, "fixture check failure") {
				t.Fatalf("reason %q does not carry the failing stage's output", message)
			}
			if flagErr != nil {
				t.Fatalf("the flag was removed after a failed stage: %v", flagErr)
			}
		})
	}
}

// `engine-sync codex agents` takes no flag for what the global file's
// `targets` already says: the source repository and the host capabilities.
func TestCodexAgentsReadsTargetsWithNoFlag(t *testing.T) {
	home, _, e := auditEnv(t)
	repo := filepath.Join(home, "clone")
	putProjectFile(t, repo, "CLAUDE.md", "Clone\n")
	putProjectFile(t, repo, "agents/astra.md", "---\nname: astra\ndescription: Astra.\ntools: Read\nrequires: [example-cap]\n---\nAstra.\n")
	role := filepath.Join(home, ".codex", "agents", "astra.toml")
	putTargets(t, home, `{"sourceRepo":`+strconvQuote(repo)+`}`)
	auditOK(t, e, "codex", "agents")
	if _, err := os.Lstat(role); !os.IsNotExist(err) {
		t.Fatalf("a role requiring an absent capability was written: %v", err)
	}
	putTargets(t, home, `{"sourceRepo":`+strconvQuote(repo)+`,"capabilities":["example-cap"]}`)
	auditOK(t, e, "codex", "agents")
	if raw, err := os.ReadFile(role); err != nil || !strings.Contains(string(raw), `name = "astra"`) {
		t.Fatalf("role from targets.sourceRepo with targets.capabilities: %v %s", err, raw)
	}
}
