package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/guard"
)

// project makes a repo that opted in (it carries .claude/codex-build.json) and
// an env whose CLAUDE_PROJECT_DIR names it.
func project(t *testing.T) (string, env) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "probe-repo")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "codex-build.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"HOME": os.Getenv("HOME"), "CLAUDE_PROJECT_DIR": root}
	return root, env{get: func(name string) string { return vars[name] }}
}

func setFlag(t *testing.T, e env, root string) string {
	t.Helper()
	flag, err := dirtyFlag(e, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(flag), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flag, []byte(root+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return flag
}

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

func TestStopWithoutTheFlagRunsNothing(t *testing.T) {
	_, e := project(t)
	ran := false
	var stdout, stderr bytes.Buffer
	code := hookStop(hookPayload{}, &stdout, &stderr, e, func([]string) (int, string) { ran = true; return 0, "" })
	if code != 0 || ran || stdout.Len() != 0 {
		t.Fatalf("code %d, ran %v, stdout %q: a clean turn runs no stage", code, ran, stdout.String())
	}
}

func TestMarkSetsTheFlagForAClaudeSourceInScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		file   string
		scope  string // "config", "codex-marker", "opencode-marker" or ""
		marked bool
	}{
		{name: "claude dir in a configured repo", file: ".claude/agents/a.md", scope: "config", marked: true},
		{name: "CLAUDE.md under a codex marker", file: "CLAUDE.md", scope: "codex-marker", marked: true},
		{name: "mcp.json under an opencode marker", file: ".mcp.json", scope: "opencode-marker", marked: true},
		{name: "not a source", file: "src/main.go", scope: "config"},
		{name: "a source out of scope", file: ".claude/agents/a.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), ".dotted-repo")
			write := func(rel, content string) {
				path := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write("README.md", "x\n")
			switch tc.scope {
			case "config":
				write(".claude/codex-build.json", "{}\n")
			case "codex-marker":
				write("AGENTS.md", "<!-- Generated by pfm codex build -->\n")
			case "opencode-marker":
				write(".opencode/opencode.jsonc", "// Generated by pfm opencode build\n{}\n")
			}
			vars := map[string]string{"HOME": os.Getenv("HOME"), "CLAUDE_PROJECT_DIR": root}
			e := env{get: func(name string) string { return vars[name] }}
			var payload hookPayload
			payload.ToolInput.FilePath = filepath.Join(root, tc.file)
			var stderr bytes.Buffer
			if code := hookMark(payload, &stderr, e); code != 0 {
				t.Fatalf("mark exited %d: %s", code, stderr.String())
			}
			flag, err := dirtyFlag(e, root)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(filepath.Base(flag), "dotted-repo-") {
				t.Fatalf("flag %s is not named for the repo, leading dot stripped", flag)
			}
			_, statErr := os.Stat(flag)
			if marked := statErr == nil; marked != tc.marked {
				t.Fatalf("flag set = %v, want %v (stderr %q)", marked, tc.marked, stderr.String())
			}
		})
	}
}

func TestStartRebuildsAStaleMirrorAndFlagsAFailedBuild(t *testing.T) {
	for _, tc := range []struct {
		name               string
		check, build       int
		wantCalls          []string
		wantFlag, wantNote bool
	}{
		{name: "fresh", check: 0, wantCalls: []string{"check"}},
		{name: "stale", check: 1, build: 0, wantCalls: []string{"check", "build"}},
		{name: "unbuildable", check: 1, build: 1, wantCalls: []string{"check", "build"}, wantFlag: true, wantNote: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, e := project(t)
			var calls []string
			runner := func(stage []string) (int, string) {
				calls = append(calls, stage[0])
				if stage[0] == "check" {
					return tc.check, ""
				}
				return tc.build, "fixture build failure\n"
			}
			var stderr bytes.Buffer
			if code := hookStart(&stderr, e, runner); code != 0 {
				t.Fatalf("start exited %d", code)
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Fatalf("calls = %q, want %q (no targets.json: no global)", calls, tc.wantCalls)
			}
			flag, _ := dirtyFlag(e, root)
			_, statErr := os.Stat(flag)
			if (statErr == nil) != tc.wantFlag {
				t.Fatalf("flag set = %v, want %v", statErr == nil, tc.wantFlag)
			}
			if strings.Contains(stderr.String(), "fixture build failure") != tc.wantNote {
				t.Fatalf("stderr %q: a failed build is named, a pass is silent", stderr.String())
			}
		})
	}
}

func TestStartRunsGlobalWhenTargetsNameIt(t *testing.T) {
	_, e := project(t)
	home, _ := e.home()
	targets := filepath.Join(home, ".config", "engine-sync", "targets.json")
	if err := os.MkdirAll(filepath.Dir(targets), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targets, []byte(`{"codexHomes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	runner := func(stage []string) (int, string) { calls = append(calls, stage[0]); return 0, "" }
	var stderr bytes.Buffer
	hookStart(&stderr, e, runner)
	if want := []string{"check", "global"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

// A repository's codex build also writes the home's global commands, the
// outputs `global` writes: a newer writer of either refuses it.
func TestANewerWriterRecordRefusesTheBuild(t *testing.T) {
	for _, recorded := range []string{"the repository", "the home's global outputs"} {
		t.Run(recorded, func(t *testing.T) {
			root, e := project(t)
			home, _ := e.home()
			key := root
			if recorded != "the repository" {
				key = "global:" + home
			}
			if err := guard.Save(e.state(home), key, "9.9.9", "/elsewhere/engine-sync"); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := run([]string{"codex", "build", root}, &stdout, &stderr, e)
			if code != 1 || !strings.Contains(stderr.String(), "NEWER-WRITER") {
				t.Fatalf("code %d, stderr %q: an older binary must refuse a root a newer one wrote", code, stderr.String())
			}
		})
	}
}

// --json is one object on stdout for every exit 0 or 1 (docs/interface.md):
// both engines' reports fold into one, and a root that cannot be resolved is
// the object's error.
func TestJSONIsOneObject(t *testing.T) {
	root, e := project(t)
	for _, args := range [][]string{
		{"check", root, "--json"}, {"build", root, "--json"}, {"doctor", root, "--json"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(args, &stdout, &stderr, e)
		decoder := json.NewDecoder(&stdout)
		var reply map[string]any
		if err := decoder.Decode(&reply); err != nil || decoder.More() || (code != 0 && code != 1) {
			t.Fatalf("%q: code %d, decode %v, more %v, stdout %q", args, code, err, decoder.More(), stdout.String())
		}
		if ok, _ := reply["ok"].(bool); ok != (code == 0) {
			t.Fatalf("%q: ok=%v disagrees with exit %d", args, reply["ok"], code)
		}
	}
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"codex", "check", "--json"}, {"opencode", "check", "--json"}} {
		var stdout, stderr bytes.Buffer
		code := run(args, &stdout, &stderr, e)
		var reply struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &reply); err != nil || code != 1 || reply.OK || reply.Error == "" {
			t.Fatalf("%q without a root: code %d, stdout %q (%v), want one object naming the error", args, code, stdout.String(), err)
		}
	}
}

func TestCommandExitCodes(t *testing.T) {
	_, e := project(t)
	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"version"}, 0},
		{[]string{}, 2},
		{[]string{"bogus"}, 2},
		{[]string{"codex", "bogus"}, 2},
		{[]string{"opencode", "bogus"}, 2},
		{[]string{"hook", "claude"}, 2},
		{[]string{"hook", "codex", "inject"}, 0},
	} {
		var stdout, stderr bytes.Buffer
		if got := run(tc.args, &stdout, &stderr, e); got != tc.want {
			t.Errorf("run %q = %d, want %d (stderr %q)", tc.args, got, tc.want, stderr.String())
		}
	}
}
