package machine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/testjail"
)

func TestSpliceRulesBlockRoundTrip(t *testing.T) {
	block := artifact.RulesBegin + "\nrule\n" + artifact.RulesEnd + "\n"
	for _, original := range []string{"", "abc", "abc\n", "abc\n\n", "abc\r\n", "a\r\nb"} {
		added, err := spliceRulesBlock(original, block)
		if err != nil {
			t.Fatalf("%q: append: %v", original, err)
		}
		if !strings.HasPrefix(added, original) || !strings.HasSuffix(added, block) {
			t.Fatalf("%q: the append changed the text outside the fence: %q", original, added)
		}
		if again, _ := spliceRulesBlock(added, block); again != added {
			t.Fatalf("%q: the same block rewrote the file: %q", original, again)
		}
		removed, err := spliceRulesBlock(added, "")
		if err != nil || removed != original {
			t.Fatalf("append then remove: %q, want %q (%v)", removed, original, err)
		}
	}
	// A block with the operator's text after it is cut out alone.
	if got, _ := spliceRulesBlock("A\n\n"+block+"B\n", ""); got != "A\n\nB\n" {
		t.Fatalf("removing a block before operator text: %q", got)
	}
	// A block an editor turned to CRLF is replaced whole, its line end too.
	crlf := strings.ReplaceAll("K\n\n"+block, "\n", "\r\n")
	if got, _ := spliceRulesBlock(crlf, block); got != "K\r\n\r\n"+block {
		t.Fatalf("replacing a CRLF block: %q", got)
	}
	if _, err := spliceRulesBlock(block+block, block); err == nil {
		t.Fatal("two fences were not refused")
	}
}

func TestGlobalRulesRefusesLinkedAgentsFile(t *testing.T) {
	home, codexHome := rulesFixture(t)
	agents := filepath.Join(codexHome, "AGENTS.md")
	real := filepath.Join(home, "dotfiles", "AGENTS.md")
	writeMachineFixture(t, real, codexKeeper)
	if err := os.Remove(agents); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, agents); err != nil {
		t.Fatal(err)
	}
	report := RunMachine(Options{Home: home, CodexHomes: []string{codexHome}})
	if report.Err == nil || !strings.Contains(report.Err.Error(), agents) {
		t.Fatalf("a linked AGENTS.md was not refused: %v", report.Err)
	}
	if got := readFixture(t, real); got != codexKeeper {
		t.Fatalf("the link's target was written:\n%s", got)
	}
}

func TestGlobalRulesDanglingLinks(t *testing.T) {
	home, codexHome := rulesFixture(t)
	rules := filepath.Join(home, ".claude", "rules")
	if err := os.Symlink(filepath.Join(home, "gone"), filepath.Join(rules, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	runRules(t, home, codexHome, false, false)
	if got := readFixture(t, filepath.Join(codexHome, "AGENTS.md")); !strings.Contains(got, "Always A.") {
		t.Fatalf("a dangling non-rule link stopped the rules:\n%s", got)
	}
	dangling := filepath.Join(rules, "lost.md")
	if err := os.Symlink(filepath.Join(home, "gone.md"), dangling); err != nil {
		t.Fatal(err)
	}
	report := RunMachine(Options{Home: home, CodexHomes: []string{codexHome}})
	if report.Err == nil || !strings.Contains(report.Err.Error(), dangling) {
		t.Fatalf("a dangling rule was not an error naming it: %v", report.Err)
	}
}

func TestGlobalRulesFenceInRuleBody(t *testing.T) {
	home, codexHome := rulesFixture(t)
	quoting := filepath.Join(home, ".claude", "rules", "quote.md")
	writeMachineFixture(t, quoting, "The fence reads "+artifact.RulesEnd+".\n")
	report := runRules(t, home, codexHome, false, false)
	if !strings.Contains(strings.Join(messages(report.Events, VerbSkip), "\n"), "RULE-FENCE "+quoting) {
		t.Fatalf("a rule quoting the fence was not skipped: %v", report.Events)
	}
	runRules(t, home, codexHome, false, false) // a second run must still read one fence
	if !hasRow(rulesRowsOf(t, home, codexHome), LevelFail, "RULE-FENCE "+quoting) {
		t.Fatal("doctor has no RULE-FENCE row")
	}
}

func TestGlobalRulesUnreadableCommands(t *testing.T) {
	if testjail.RanUnprivileged(t) {
		return
	}
	home, codexHome := rulesFixture(t)
	commands := filepath.Join(home, ".claude", "commands")
	if err := os.Chmod(commands, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(commands, 0o755) })
	report := RunMachine(Options{Home: home, CodexHomes: []string{codexHome}, skipRules: false})
	if report.Err == nil || !strings.Contains(report.Err.Error(), "/command roster") {
		t.Fatalf("an unreadable command roster did not fail the rules: %v", report.Err)
	}
	if strings.Contains(readFixture(t, filepath.Join(codexHome, "AGENTS.md")), "/tidy") {
		t.Fatal("a rule shipped its /command untransformed")
	}
	if !hasRow(rulesRowsOf(t, home, codexHome), LevelFail, "global-rules ERROR", "/command roster") {
		t.Fatal("doctor has no ERROR row for the unreadable roster")
	}
}

// A rule-control rule (visible-to, no paths) stays always-on, compiles without
// its frontmatter, and doctor names the loops it over-reaches as a row, never
// a failure: Codex and OpenCode have no per-loop delivery.
func TestGlobalRulesVisibleTo(t *testing.T) {
	home, codexHome := rulesFixture(t)
	rules := filepath.Join(home, ".claude", "rules")
	targeted := filepath.Join(rules, "main-only.md")
	writeMachineFixture(t, targeted, "---\nvisible-to:\n  include: [main]\n  exclude: [\"reviewer-*\"]\n---\nMain only.\n")
	scoped := filepath.Join(rules, "scoped-main.md")
	writeMachineFixture(t, scoped, "---\npaths: [\"src/**\"]\nvisible-to: {include: [main]}\n---\nScoped main.\n")
	report := runRules(t, home, codexHome, false, false)
	if skipped := strings.Join(messages(report.Events, VerbSkip), "\n"); strings.Contains(skipped, "RULE-") {
		t.Fatalf("a visible-to rule was skipped: %v", skipped)
	}
	want := "<!-- rule: ~/.claude/rules/main-only.md -->\nMain only.\n"
	for _, path := range []string{filepath.Join(codexHome, "AGENTS.md"), compiledRulesPath(home)} {
		got := readFixture(t, path)
		if !strings.Contains(got, want) || strings.Contains(got, "visible-to") || strings.Contains(got, "include:") || strings.Contains(got, "Scoped main.") {
			t.Fatalf("%s does not hold the targeted rule always-on with its frontmatter stripped:\n%s", path, got)
		}
	}
	rows := rulesRowsOf(t, home, codexHome)
	for _, row := range rows {
		if row.Level == LevelFail {
			t.Errorf("a visible-to rule failed doctor: %v", row)
		}
	}
	if !hasRow(rows, LevelOK, "VISIBLE-TO "+targeted, "every Codex and OpenCode loop") {
		t.Errorf("doctor does not name the visible-to gap: %v", rows)
	}
	if hasRow(rows, LevelOK, "VISIBLE-TO "+scoped) || !hasRow(rows, LevelWarn, "PATH-SCOPED "+scoped) {
		t.Errorf("a path-scoped visible-to rule is not PATH-SCOPED alone: %v", rows)
	}
	if hasRow(rows, LevelOK, "VISIBLE-TO", filepath.Join(rules, "a.md")) {
		t.Errorf("a rule with no visible-to is named: %v", rows)
	}
}
