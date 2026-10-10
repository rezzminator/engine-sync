package codex

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestMachineInspectionValidatesOnlyCodexRoles(t *testing.T) {
	home := testjail.Scratch(t)
	cfg := config.Default()
	p := source.Project{Scope: source.MachineScope, Home: home, AgentSourceDir: filepath.Join(home, "source"), CodexHomes: []string{filepath.Join(home, ".codex")}, ClaudeConfigDirs: []string{filepath.Join(home, ".claude")}}
	for _, dir := range []string{filepath.Join(home, ".claude", "agents"), filepath.Join(home, cfg.Machine.GeneratedAgentsDir)} {
		path := filepath.Join(dir, "unrelated.txt")
		writeCodexFixture(t, path, "private data\n")
		if err := os.Chmod(path, 0000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, 0600) })
		if _, err := os.ReadFile(path); err == nil {
			t.Skip("permission proof requires a non-root user")
		}
	}
	role := filepath.Join(home, ".codex", "agents", "keeper.toml")
	writeCodexFixture(t, role, "broken = [\n")
	target := Target{}
	outputs, problems := reconcile.Inspect(target.Roots(p, cfg), nil)
	if len(problems) != 0 {
		t.Fatalf("unrelated machine entries were read: %v", problems)
	}
	diagnostics := target.Validate(outputs)
	if len(diagnostics) != 1 || diagnostics[0].Path != role {
		t.Fatalf("Codex role validation lost: %+v", diagnostics)
	}
}

// TestClassifyGlobalRoleSeparatesOursFromTheirs walks every state the install
// path can meet at a role's path. The line that matters most is the foreign
// one: a regular file with no generated marker belongs to the operator, and
// ApplyGlobalRole must leave it byte-for-byte alone whatever its name.
func TestClassifyGlobalRoleSeparatesOursFromTheirs(t *testing.T) {
	dir := testjail.Scratch(t)
	legacy := filepath.Join(dir, "legacy")
	want := []byte(globalRoleHeader("templates/global/agents/alpha.md") + "name = \"alpha\"\n")

	cases := []struct {
		name  string
		setup func(target string)
		state GlobalRoleState
	}{
		{"missing", func(string) {}, GlobalRoleMissing},
		{"current", func(target string) { writeCodexFixture(t, target, string(want)) }, GlobalRoleCurrent},
		{
			"stale",
			func(target string) {
				writeCodexFixture(t, target, globalRoleHeader("templates/global/agents/alpha.md")+"name = \"old\"\n")
			},
			GlobalRoleStale,
		},
		{"owned-link", func(target string) {
			source := filepath.Join(legacy, "alpha.toml")
			writeCodexFixture(t, source, "name = \"alpha\"\n")
			if err := os.Symlink(source, target); err != nil {
				t.Fatal(err)
			}
		}, GlobalRoleOwnedLink},
		{"foreign file", func(target string) {
			writeCodexFixture(t, target, "name = \"alpha\"\ndescription = \"the operator's own\"\n")
		}, GlobalRoleForeign},
		{"foreign link", func(target string) {
			source := filepath.Join(dir, "elsewhere.toml")
			writeCodexFixture(t, source, "name = \"alpha\"\n")
			if err := os.Symlink(source, target); err != nil {
				t.Fatal(err)
			}
		}, GlobalRoleForeign},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			target := filepath.Join(testjail.Scratch(t), "alpha.toml")
			testCase.setup(target)
			before, _ := os.ReadFile(target)

			state, _, err := ClassifyGlobalRole(target, want, []string{legacy})
			if err != nil {
				t.Fatalf("ClassifyGlobalRole: %v", err)
			}
			if state != testCase.state {
				t.Fatalf("state=%s, want %s", state, testCase.state)
			}

			if err := ApplyGlobalRole(target, want, state); err != nil {
				t.Fatalf("ApplyGlobalRole: %v", err)
			}
			if state == GlobalRoleForeign {
				// os.ReadFile, not ReadGlobalRoleFile: a foreign entry may be
				// the very symlink Codex refuses, and the question here is
				// only whether apply left it exactly as the operator had it.
				after, afterErr := os.ReadFile(target)
				if afterErr != nil || !bytes.Equal(after, before) {
					t.Fatalf("a foreign role was touched: read %q err=%v, want %q", after, afterErr, before)
				}
				return
			}
			got, err := ReadGlobalRoleFile(target)
			if err != nil {
				t.Fatalf("after apply, Codex cannot read the role: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("after apply, role = %q, want %q", got, want)
			}
		})
	}
}

// TestGeneratedGlobalRoleAnchorsOnTheFirstLine pins the ownership proof to
// where pfm writes it. A body that merely quotes the marker phrase is not a
// file pfm owns, and treating it as one would hand an operator's role to the
// retire path.
func TestGeneratedGlobalRoleAnchorsOnTheFirstLine(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		owned   bool
	}{
		{"today's header", globalRoleHeader("templates/global/agents/alpha.md"), true},
		{"the retired build-codex.mjs header", "# " + artifact.LegacyCodexScript + " from x\nname = \"a\"\n", true},
		{"no marker", "name = \"a\"\n", false},
		{"the phrase buried in a body", "name = \"a\"\ndeveloper_instructions = \"\"\"\n" + artifact.Prefix + "\n", false},
		{"empty", "", false},
	} {
		if got := GeneratedGlobalRole([]byte(testCase.content)); got != testCase.owned {
			t.Errorf("GeneratedGlobalRole(%s) = %v, want %v", testCase.name, got, testCase.owned)
		}
	}
}
