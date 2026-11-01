package source_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
)

// Each project's own .claude/rules load, keyed by the project, sorted by
// path; a project without a rules directory has no key; a rule that cannot
// be read carries its error, its siblings still loaded, never an empty set.
func TestLoadProjectReadsEachProjectsRules(t *testing.T) {
	root := testjail.Scratch(t)
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rules := filepath.Join(".claude", "rules")
	write("CLAUDE.md", "# Root\n")
	write(filepath.Join(rules, "z.md"), "Always.\n")
	write(filepath.Join(rules, "a", "scoped.md"), "---\npaths: \"src/**\"\nvisible-to: [main]\n---\nScoped.\n")
	write(filepath.Join(rules, "notes.txt"), "not a rule\n")
	write(filepath.Join("app", "CLAUDE.md"), "# App\n")
	write(filepath.Join("app", rules, "child.md"), "---\npaths:\n  - \"app/**\"\n  - \"lib/**\"\n---\nChild.\n")
	write(filepath.Join("lib", "CLAUDE.md"), "# Lib\n")
	write(filepath.Join("lib", rules, "broken.md"), "---\npaths:\n  - a: [unclosed\n---\nBroken.\n")
	cfg := config.Default()
	cfg.Codex.Projects = []string{".", "app", "lib"}

	p := source.LoadProject(root, filepath.Join(root, "home"), cfg)
	want := map[string][]source.Rule{
		".": {
			{Path: filepath.Join(root, rules, "a", "scoped.md"), Rel: "a/scoped.md", Scope: []string{"src/**"}, VisibleTo: true, Body: "Scoped.\n"},
			{Path: filepath.Join(root, rules, "z.md"), Rel: "z.md", Body: "Always.\n"},
		},
		"app": {
			{Path: filepath.Join(root, "app", rules, "child.md"), Rel: "child.md", Scope: []string{"app/**", "lib/**"}, Body: "Child.\n"},
		},
	}
	// A header that does not parse is one rule carrying its error, nothing
	// parsed from it, for the compiler to name.
	broken := p.Rules["lib"]
	const brokenErr = "line 2 — did not find expected ',' or ']'"
	if len(broken) != 1 || broken[0].ParseErr == nil || !reflect.DeepEqual(broken[0], source.Rule{Path: filepath.Join(root, "lib", rules, "broken.md"), Rel: "broken.md", ParseErr: broken[0].ParseErr}) || broken[0].ParseErr.Error() != brokenErr {
		t.Fatalf("lib rules = %#v, want one broken.md rule with ParseErr %q", broken, brokenErr)
	}
	delete(p.Rules, "lib")
	if len(p.Problems) != 0 || !reflect.DeepEqual(p.Rules, want) {
		t.Fatalf("rules = %#v problems=%q, want %#v", p.Rules, p.Problems, want)
	}

	if testjail.RanUnprivileged(t) {
		return
	}
	unreadable := filepath.Join(root, rules, "z.md")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })
	p = source.LoadProject(root, filepath.Join(root, "home"), cfg)
	got := p.Rules["."]
	readErr := "open " + unreadable + ": permission denied"
	if len(got) != 2 || got[1].ReadErr == nil || got[1].ReadErr.Error() != readErr ||
		!reflect.DeepEqual(got[0], want["."][0]) || !reflect.DeepEqual(got[1], source.Rule{Path: unreadable, Rel: "z.md", ReadErr: got[1].ReadErr}) {
		t.Fatalf("root rules beside an unreadable rule = %#v, want scoped.md whole and z.md with ReadErr %q", got, readErr)
	}
	if len(p.Problems) != 0 || len(p.RulesFailed) != 0 || len(p.Rules["app"]) != 1 {
		t.Fatalf("problems=%q rules failed=%q app rules=%#v, want none, none and app's one", p.Problems, p.RulesFailed, p.Rules["app"])
	}
}
