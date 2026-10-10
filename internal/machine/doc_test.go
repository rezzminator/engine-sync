package machine

import (
	"errors"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The plan is the apply's preview: the same change and retire lines, nothing
// written by the plan, and the apply's result on disk.
func TestPlanListsExactlyWhatTheApplyDoes(t *testing.T) {
	home, repo := fixture(t)
	writeMachineFixture(t, filepath.Join(home, ".codex", "agents", "old.toml"), generatedRole)
	writeMachineFixture(t, filepath.Join(home, ".codex", "agents", "mine.toml"), "name = \"mine\"\n")
	options := Options{Home: home, SourceRepo: repo, Plan: true}
	before := tree(t, home)
	plan := RunMachine(options)
	if plan.Err != nil || !plan.OK {
		t.Fatalf("plan err=%v ok=%v events=%#v", plan.Err, plan.OK, plan.Events)
	}
	if after := tree(t, home); !reflect.DeepEqual(before, after) {
		t.Fatalf("the plan wrote to disk")
	}
	options.Plan = false
	apply := RunMachine(options)
	if apply.Err != nil || !apply.OK {
		t.Fatalf("apply err=%v events=%#v", apply.Err, apply.Events)
	}
	for _, verb := range []string{VerbChange, VerbRetire} {
		if got, want := messages(apply.Events, verb), messages(plan.Events, verb); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s lines differ:\nplan  %q\napply %q", verb, want, got)
		}
	}
	role := filepath.Join(home, ".codex", "agents", "gamma.toml")
	raw, err := os.ReadFile(role)
	if err != nil || !codex.GeneratedGlobalRole(raw) {
		t.Fatalf("role %s = %q, %v; want a generated role file", role, raw, err)
	}
	link := filepath.Join(home, ".claude", "agents", "gamma.md")
	if target, err := os.Readlink(link); err != nil || target != filepath.Join(repo, "agents", "gamma.md") {
		t.Fatalf("claude link %s -> %q, %v", link, target, err)
	}
	variant := filepath.Join(paths.GeneratedClaudeAgentsDir(home), "gamma-deep.md")
	if _, err := os.Stat(variant); err != nil {
		t.Fatalf("rendered variant %s: %v", variant, err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex", "agents", "old.toml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("orphaned generated role survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "agents", "mine.toml")); err != nil {
		t.Fatalf("a foreign role was touched: %v", err)
	}
	if got := messages(apply.Events, VerbOK); len(got) != 2 ||
		!strings.HasPrefix(got[0], "Codex global agents compiled=") ||
		!strings.HasPrefix(got[1], "Codex global commands wrote=") {
		t.Fatalf("ok lines = %q", got)
	}
	if again := RunMachine(options); len(messages(again.Events, VerbChange)) != 0 || len(messages(again.Events, VerbRetire)) != 0 {
		t.Fatalf("a second apply changed something: %#v", again.Events)
	}
}
