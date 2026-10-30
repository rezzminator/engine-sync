package machine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/artifact"
)

func TestGlobalRulesOpenCodeEntry(t *testing.T) {
	home, codexHome := rulesFixture(t)
	runRules(t, home, codexHome, false, false)
	compiled := compiledRulesPath(home)
	if got := readFixture(t, compiled); got != artifact.GlobalMarker(artifact.Markdown, "~/.claude/rules")+"\n\n"+wantRulesBody() {
		t.Fatalf("compiled rules:\n%s", got)
	}
	want := "{\n  // operator note\n  \"instructions\": [\"/other/opencode.md\",\"" + compiled + "\"],\n  \"model\": \"m\"\n}\n"
	if got := readFixture(t, openCodeConfigPath(home)); got != want {
		t.Fatalf("OpenCode config:\n%s\nwant:\n%s", got, want)
	}
	if _, err := os.Lstat(filepath.Join(home, ".config", "opencode", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("the global OpenCode AGENTS.md was written: %v", err)
	}

	runRules(t, home, codexHome, false, true)
	if got := readFixture(t, openCodeConfigPath(home)); got != openCodeConfig {
		t.Fatalf("uninstall left the OpenCode config:\n%s\nwant:\n%s", got, openCodeConfig)
	}
	if _, err := os.Lstat(compiled); !os.IsNotExist(err) {
		t.Fatalf("uninstall kept the compiled rules: %v", err)
	}
	if got := readFixture(t, filepath.Join(codexHome, "AGENTS.md")); got != codexKeeper {
		t.Fatalf("uninstall left AGENTS.md:\n%q", got)
	}
}

func TestGlobalRulesOpenCodeFailedEntryKeepsFile(t *testing.T) {
	home, codexHome := rulesFixture(t)
	runRules(t, home, codexHome, false, false)
	compiled := compiledRulesPath(home)
	broken := strings.Replace(readFixture(t, openCodeConfigPath(home)), `"model": "m"`, `"model": `, 1)
	writeMachineFixture(t, openCodeConfigPath(home), broken)
	report := RunMachine(Options{Home: home, CodexHomes: []string{codexHome}, Uninstall: true})
	if report.Err == nil {
		t.Fatal("uninstall over a config it cannot parse succeeded")
	}
	if _, err := os.Lstat(compiled); err != nil {
		t.Fatalf("the compiled file its entry still names was removed: %v", err)
	}
}

func TestGlobalRulesOpenCodeOperatorEntries(t *testing.T) {
	home, codexHome := rulesFixture(t)
	compiled := compiledRulesPath(home)
	// The operator lists the compiled file by hand, beside an entry with an "&".
	config := "{\n  \"instructions\": [\"/a&b.md\", \"" + compiled + "\"]\n}\n"
	writeMachineFixture(t, openCodeConfigPath(home), config)
	runRules(t, home, codexHome, false, false)
	if got := readFixture(t, openCodeConfigPath(home)); got != config {
		t.Fatalf("a hand-listed entry was rewritten:\n%s", got)
	}
	runRules(t, home, codexHome, false, true)
	if got := readFixture(t, openCodeConfigPath(home)); got != config {
		t.Fatalf("uninstall removed the operator's own entry:\n%s", got)
	}

	writeMachineFixture(t, openCodeConfigPath(home), "{\"instructions\": [\"/a&b.md\"]}\n")
	runRules(t, home, codexHome, false, false)
	if got := readFixture(t, openCodeConfigPath(home)); !strings.Contains(got, `"/a&b.md"`) {
		t.Fatalf("an operator entry was re-escaped:\n%s", got)
	}
}

func TestGlobalRulesOpenCodeEmptyConfigKeepsMode(t *testing.T) {
	home, codexHome := rulesFixture(t)
	path := openCodeConfigPath(home)
	writeMachineFixture(t, path, "")
	runRules(t, home, codexHome, false, false)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("an empty config lost its mode: %v %v", info.Mode(), err)
	}
}

func TestGlobalRulesOpenCodeOwnershipSurvivesLinkChange(t *testing.T) {
	home, codexHome := rulesFixture(t)
	path := openCodeConfigPath(home)
	real := filepath.Join(home, "dotfiles", "opencode.jsonc")
	writeMachineFixture(t, real, openCodeConfig)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	runRules(t, home, codexHome, false, false)
	// The operator replaces the link with a regular copy of its target.
	copied := readFixture(t, real)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeMachineFixture(t, path, copied)
	runRules(t, home, codexHome, false, true)
	if got := readFixture(t, path); got != openCodeConfig {
		t.Fatalf("uninstall left engine-sync's entry in the copied config:\n%s", got)
	}
}
