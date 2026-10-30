package machine

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/testjail"
)

const codexKeeper = "# Keeper\n\nhand text\n\n<!-- BEGIN collie -->\nx\n<!-- END collie -->\n"

// openCodeConfig carries an operator comment and another tool's entry,
// both of which the rules entry must leave alone.
const openCodeConfig = "{\n  // operator note\n  \"instructions\": [\"/other/opencode.md\"],\n  \"model\": \"m\"\n}\n"

// rulesFixture is one home with three always-on global rules (one nested),
// one path-scoped rule, a command the rules name, a Codex home holding a
// hand-written AGENTS.md, and an OpenCode config with another tool's entry.
func rulesFixture(t *testing.T) (home, codexHome string) {
	t.Helper()
	home = testjail.Scratch(t)
	t.Setenv("ENGINE_SYNC_HOME", filepath.Join(home, "es-home"))
	t.Setenv("ENGINE_SYNC_STATE", filepath.Join(home, "es-state"))
	codexHome = filepath.Join(home, ".codex")
	rules := filepath.Join(home, ".claude", "rules")
	writeMachineFixture(t, filepath.Join(home, ".claude", "engine-sync.json"), `{"version":1,"codex":{"modelMap":{"sonnet":"gpt-5"}}}`)
	writeMachineFixture(t, filepath.Join(home, ".claude", "commands", "tidy.md"), "---\ndescription: Tidy.\n---\nTidy up.\n")
	writeMachineFixture(t, filepath.Join(rules, "b.md"), "---\ndescription: ignored\n---\nRun /tidy with model: sonnet.\n")
	writeMachineFixture(t, filepath.Join(rules, "a.md"), "Always A.\n")
	writeMachineFixture(t, filepath.Join(rules, "sub", "c.md"), "C nested.\n")
	writeMachineFixture(t, filepath.Join(rules, "scoped.md"), "---\npaths:\n  - \"src/**\"\n---\nScoped.\n")
	writeMachineFixture(t, filepath.Join(codexHome, "AGENTS.md"), codexKeeper)
	writeMachineFixture(t, openCodeConfigPath(home), openCodeConfig)
	return home, codexHome
}

func compiledRulesPath(home string) string {
	return filepath.Join(paths.EngineSyncHome(paths.Getenv, home), "rules", "global-always-on.md")
}

func runRules(t *testing.T, home, codexHome string, plan, uninstall bool) Report {
	t.Helper()
	report := RunMachine(Options{Home: home, CodexHomes: []string{codexHome}, Plan: plan, Uninstall: uninstall})
	if report.Err != nil {
		t.Fatalf("global: %v\n%v", report.Err, report.Events)
	}
	return report
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func wantRulesBody() string {
	return "<!-- rule: ~/.claude/rules/a.md -->\nAlways A.\n\n" +
		"<!-- rule: ~/.claude/rules/b.md -->\nRun $tidy with model: gpt-5.\n\n" +
		"<!-- rule: ~/.claude/rules/sub/c.md -->\nC nested.\n"
}

func TestGlobalRulesCodexBlock(t *testing.T) {
	home, codexHome := rulesFixture(t)
	agents := filepath.Join(codexHome, "AGENTS.md")
	runRules(t, home, codexHome, false, false)
	block := artifact.RulesBegin + "\n" + wantRulesBody() + artifact.RulesEnd + "\n"
	if got, want := readFixture(t, agents), codexKeeper+"\n"+block; got != want {
		t.Fatalf("AGENTS.md:\n%s\nwant:\n%s", got, want)
	}

	second := runRules(t, home, codexHome, false, false)
	for _, event := range second.Events {
		if strings.Contains(event.Message, agents) {
			t.Fatalf("an unchanged rule set rewrote AGENTS.md: %v", event)
		}
	}

	writeMachineFixture(t, filepath.Join(home, ".claude", "rules", "a.md"), "Always A, edited.\n")
	runRules(t, home, codexHome, false, false)
	if got := readFixture(t, agents); !strings.HasPrefix(got, codexKeeper+"\n"+artifact.RulesBegin+"\n") || !strings.Contains(got, "Always A, edited.\n") {
		t.Fatalf("an edited rule did not replace the block in place:\n%s", got)
	}

	for _, name := range []string{"a.md", "b.md", filepath.Join("sub", "c.md")} {
		if err := os.Remove(filepath.Join(home, ".claude", "rules", name)); err != nil {
			t.Fatal(err)
		}
	}
	runRules(t, home, codexHome, false, false)
	if got := readFixture(t, agents); got != codexKeeper {
		t.Fatalf("no always-on rule left the block behind:\n%q\nwant:\n%q", got, codexKeeper)
	}
}

func TestGlobalRulesCreateAndRemoveAgentsFile(t *testing.T) {
	home, codexHome := rulesFixture(t)
	if err := os.Remove(filepath.Join(codexHome, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	runRules(t, home, codexHome, false, false)
	want := artifact.RulesBegin + "\n" + wantRulesBody() + artifact.RulesEnd + "\n"
	if got := readFixture(t, filepath.Join(codexHome, "AGENTS.md")); got != want {
		t.Fatalf("created AGENTS.md:\n%s\nwant:\n%s", got, want)
	}
	runRules(t, home, codexHome, false, true)
	if _, err := os.Lstat(filepath.Join(codexHome, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("uninstall left the AGENTS.md it created holding nothing: %v", err)
	}
}

func TestGlobalRulesPlanWritesNothing(t *testing.T) {
	home, codexHome := rulesFixture(t)
	before := tree(t, home)
	report := runRules(t, home, codexHome, true, false)
	if after := tree(t, home); !maps.Equal(after, before) {
		t.Fatalf("a plan wrote:\nbefore %v\nafter  %v", before, after)
	}
	for _, path := range []string{filepath.Join(codexHome, "AGENTS.md"), compiledRulesPath(home), openCodeConfigPath(home)} {
		named := false
		for _, event := range report.Events {
			named = named || (event.Verb == VerbChange && strings.Contains(event.Message, path))
		}
		if !named {
			t.Errorf("the plan does not name the write of %s: %v", path, report.Events)
		}
	}
}

func rulesRowsOf(t *testing.T, home, codexHome string) []Row {
	t.Helper()
	var rows []Row
	for _, row := range Doctor(DoctorOptions{Home: home, CodexHomes: []string{codexHome}}) {
		if strings.Contains(row.Message, "rules") || strings.HasPrefix(row.Message, "RULE-") || strings.HasPrefix(row.Message, "GLOBAL-BUDGET") {
			rows = append(rows, row)
		}
	}
	return rows
}

func hasRow(rows []Row, level string, parts ...string) bool {
	for _, row := range rows {
		match := row.Level == level
		for _, part := range parts {
			match = match && strings.Contains(row.Message, part)
		}
		if match {
			return true
		}
	}
	return false
}

func TestGlobalRulesDoctor(t *testing.T) {
	home, codexHome := rulesFixture(t)
	agents := filepath.Join(codexHome, "AGENTS.md")
	rows := rulesRowsOf(t, home, codexHome)
	for _, want := range [][]string{{agents, "MISSING"}, {compiledRulesPath(home), "MISSING"}, {openCodeConfigPath(home), "MISSING"}} {
		if !hasRow(rows, LevelFail, want...) {
			t.Errorf("before install, no fail row naming %v: %v", want, rows)
		}
	}
	if !hasRow(rows, LevelWarn, "PATH-SCOPED", filepath.Join(home, ".claude", "rules", "scoped.md")) {
		t.Errorf("the path-scoped rule is not named as undelivered: %v", rows)
	}

	runRules(t, home, codexHome, false, false)
	for _, row := range rulesRowsOf(t, home, codexHome) {
		if row.Level == LevelFail {
			t.Errorf("after install, a failing rules row: %v", row)
		}
	}

	writeMachineFixture(t, filepath.Join(home, ".claude", "rules", "a.md"), "Always A, edited.\n")
	rows = rulesRowsOf(t, home, codexHome)
	if !hasRow(rows, LevelFail, agents, "STALE") || !hasRow(rows, LevelFail, compiledRulesPath(home), "STALE") {
		t.Errorf("an edited rule is not STALE on both engines: %v", rows)
	}

	writeMachineFixture(t, filepath.Join(codexHome, "AGENTS.override.md"), "override\n")
	if rows = rulesRowsOf(t, home, codexHome); !hasRow(rows, LevelFail, agents, "SHADOWED") {
		t.Errorf("an AGENTS.override.md is not SHADOWED: %v", rows)
	}

	bad := filepath.Join(home, ".claude", "rules", "bad.md")
	writeMachineFixture(t, bad, "---\npaths:\n  - a: [unclosed\n---\nBad body.\n")
	writeMachineFixture(t, filepath.Join(home, ".claude", "rules", "big.md"), strings.Repeat("x", 9000)+"\n")
	rows = rulesRowsOf(t, home, codexHome)
	if !hasRow(rows, LevelFail, "RULE-FRONTMATTER "+bad+": ") {
		t.Errorf("an unparseable rule is not RULE-FRONTMATTER: %v", rows)
	}
	if !hasRow(rows, LevelWarn, "GLOBAL-BUDGET ") {
		t.Errorf("a set over 8 KiB has no GLOBAL-BUDGET row: %v", rows)
	}
	runRules(t, home, codexHome, false, false)
	if strings.Contains(readFixture(t, agents), "Bad body.") {
		t.Fatal("a rule whose frontmatter does not parse was compiled as always-on")
	}
}

func TestGlobalRulesUnreadableDir(t *testing.T) {
	if testjail.RanUnprivileged(t) {
		return
	}
	home, codexHome := rulesFixture(t)
	sub := filepath.Join(home, ".claude", "rules", "sub")
	if err := os.Chmod(sub, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	if rows := rulesRowsOf(t, home, codexHome); !hasRow(rows, LevelFail, "ERROR", sub) {
		t.Errorf("an unreadable rules dir is not an ERROR row: %v", rows)
	}
	report := RunMachine(Options{Home: home, CodexHomes: []string{codexHome}})
	if report.Err == nil || !strings.Contains(report.Err.Error(), sub) {
		t.Fatalf("global over an unreadable rules dir did not fail naming it: %v", report.Err)
	}
	if got := readFixture(t, filepath.Join(codexHome, "AGENTS.md")); got != codexKeeper {
		t.Fatalf("global over an unreadable rules dir wrote AGENTS.md:\n%s", got)
	}
}
