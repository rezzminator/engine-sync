package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewReconcileSafety(t *testing.T) {
	for _, name := range []string{"foreign directory child", "foreign source alias", "scratch orphan", "region mode", "region external link", "symlink parent", "duplicate outputs", "nested roots"} {
		t.Run(name, func(t *testing.T) {
			root := testjail.Scratch(t)
			roots := []artifact.Root{{Path: root, SourceRoots: []string{root}}}
			var outputs []artifact.Artifact
			put := func(rel, body string) {
				t.Helper()
				p := filepath.Join(root, rel)
				if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(p, []byte(body), 0600); e != nil {
					t.Fatal(e)
				}
			}
			switch name {
			case "foreign directory child":
				put("x/SKILL.md", artifact.Marker(artifact.Markdown, "old"))
				put("x/notes.md", "private")
			case "foreign source alias":
				put("source.md", "user")
				if e := os.Symlink(filepath.Join(root, "source.md"), filepath.Join(root, "alias.md")); e != nil {
					t.Fatal(e)
				}
			case "scratch orphan":
				put(".x.md.tmp-123", artifact.Marker(artifact.Markdown, "x"))
			case "region mode":
				put("config.toml", "old")
				outputs = []artifact.Artifact{{Path: filepath.Join(root, "config.toml"), Kind: artifact.Region, Content: []byte("new")}}
			case "region external link":
				outside := filepath.Join(testjail.Scratch(t), "config.toml")
				if e := os.WriteFile(outside, []byte("outside"), 0600); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(outside, filepath.Join(root, "config.toml")); e != nil {
					t.Fatal(e)
				}
				outputs = []artifact.Artifact{{Path: filepath.Join(root, "config.toml"), Kind: artifact.Region, Content: []byte("new")}}
			case "symlink parent":
				outside := testjail.Scratch(t)
				if e := os.Symlink(outside, filepath.Join(root, "agents")); e != nil {
					t.Fatal(e)
				}
				roots = []artifact.Root{{Path: filepath.Join(root, "agents")}}
				outputs = []artifact.Artifact{{Path: filepath.Join(root, "agents", "reader.toml"), Content: []byte(artifact.Marker(artifact.TOML, "x"))}}
			case "duplicate outputs":
				path := filepath.Join(root, "reader.toml")
				outputs = []artifact.Artifact{{Path: path, Content: []byte(artifact.Marker(artifact.TOML, "one"))}, {Path: path, Content: []byte(artifact.Marker(artifact.TOML, "two"))}}
			case "nested roots":
				roots = append(roots, artifact.Root{Path: filepath.Join(root, "inner")})
				outputs = []artifact.Artifact{{Path: filepath.Join(root, "inner", "x.md"), Content: []byte(artifact.Marker(artifact.Markdown, "x"))}}
			}
			got := reconcile.Run(outputs, reconcile.Build, roots)
			switch name {
			case "foreign directory child":
				raw, e := os.ReadFile(filepath.Join(root, "x", "notes.md"))
				if e != nil || string(raw) != "private" {
					t.Fatalf("foreign child lost: %+v %v", got, e)
				}
			case "foreign source alias":
				if _, e := os.Lstat(filepath.Join(root, "alias.md")); e != nil {
					t.Fatalf("user alias lost: %+v", got)
				}
			case "scratch orphan":
				if _, e := os.Lstat(filepath.Join(root, ".x.md.tmp-123")); e != nil {
					t.Fatalf("scratch lost: %+v", got)
				}
			case "region mode":
				info, e := os.Stat(filepath.Join(root, "config.toml"))
				if e != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("region mode changed: %+v", got)
				}
			case "region external link":
				info, e := os.Lstat(filepath.Join(root, "config.toml"))
				if e != nil || info.Mode()&os.ModeSymlink == 0 || len(got.Problems) == 0 {
					t.Fatalf("foreign keeper overwritten: %+v", got)
				}
			case "symlink parent", "duplicate outputs":
				if got.Wrote != 0 || len(got.Problems) == 0 {
					t.Fatalf("unsafe writes: %+v", got)
				}
			case "nested roots":
				if got.Deleted != 0 {
					t.Fatalf("own output deleted: %+v", got)
				}
			}
		})
	}
}

func TestReviewOutputBoundaryCoversAncestors(t *testing.T) {
	home := testjail.Scratch(t)
	outside := testjail.Scratch(t)
	if err := os.Symlink(outside, filepath.Join(home, "parent")); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "parent", "nested", "outputs")
	file := filepath.Join(root, "role.md")
	_, _, problems := reconcile.Prepare([]artifact.Artifact{{Path: file, Content: []byte(artifact.Marker(artifact.Markdown, "source"))}}, []artifact.Root{{Path: root, Boundary: home}})
	if len(problems) == 0 {
		t.Fatal("symlink above managed directory escaped boundary")
	}
}

func TestReviewEmptyRenderKeepsOrphansBeyondAncestorLink(t *testing.T) {
	home := testjail.Scratch(t)
	outside := testjail.Scratch(t)
	dir := filepath.Join(outside, "nested", "outputs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(dir, "old.md")
	if err := os.WriteFile(owned, []byte(artifact.Marker(artifact.Markdown, "old")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "parent")); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "parent", "nested", "outputs")
	for _, mode := range []reconcile.Mode{reconcile.Build, reconcile.Plan, reconcile.Check} {
		result := reconcile.Run(nil, mode, []artifact.Root{{Path: root, Boundary: home}})
		if _, err := os.Stat(owned); err != nil {
			t.Fatalf("empty render removed outside orphan: %+v: %v", result, err)
		}
		if len(result.Problems) == 0 {
			t.Fatal("empty render did not report the ancestor symlink")
		}
		if len(result.Actions) != 0 || len(result.Rebuildable) != 0 {
			t.Fatalf("unsafe root inspected after boundary refusal (mode %v): %+v", mode, result)
		}
	}
}

func TestReviewUnreadableOrphanIsFailure(t *testing.T) {
	root := testjail.Scratch(t)
	if err := os.MkdirAll(filepath.Join(root, "odd", "SKILL.md"), 0755); err != nil {
		t.Fatal(err)
	}
	result := reconcile.Run(nil, reconcile.Check, []artifact.Root{{Path: root}})
	if len(result.Problems) == 0 {
		t.Fatalf("ownership read error rendered success: %#v", result)
	}
}
func TestReviewPlanNamesForeignOrphanChildren(t *testing.T) {
	root := testjail.Scratch(t)
	skill := filepath.Join(root, "old")
	if err := os.Mkdir(skill, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte(artifact.Marker(artifact.Markdown, "source")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "notes.txt"), []byte("user"), 0644); err != nil {
		t.Fatal(err)
	}
	result := reconcile.Run(nil, reconcile.Plan, []artifact.Root{{Path: root}})
	found := false
	for _, problem := range result.Problems {
		if strings.Contains(problem, "CONFLICT") && strings.Contains(problem, "notes.txt") {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan hides preserved foreign child: %#v", result)
	}
}

func TestReviewRetirementChecksAncestorBoundary(t *testing.T) {
	home := testjail.Scratch(t)
	outside := testjail.Scratch(t)
	dir := filepath.Join(outside, "agents")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(dir, "old.toml")
	if err := os.WriteFile(owned, []byte(artifact.Marker(artifact.TOML, "old")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".codex")); err != nil {
		t.Fatal(err)
	}
	result := reconcile.RetireOwned(filepath.Join(home, ".codex", "agents", "old.toml"), reconcile.Build, home)
	if len(result.Problems) == 0 {
		t.Fatal("retirement followed ancestor symlink outside its home")
	}
	if _, err := os.Stat(owned); err != nil {
		t.Fatal("outside owned file removed", err)
	}
}

func TestReviewRebuildNamesReplacedGeneratedContent(t *testing.T) {
	root := testjail.Scratch(t)
	path := filepath.Join(root, "role.toml")
	if err := os.WriteFile(path, []byte(artifact.Marker(artifact.TOML, "source")+"\nhand edited"), 0600); err != nil {
		t.Fatal(err)
	}
	result := reconcile.Run([]artifact.Artifact{{Path: path, Content: []byte(artifact.Marker(artifact.TOML, "source") + "\nfrom source")}}, reconcile.Build, []artifact.Root{{Path: root}})
	if len(result.Warnings) == 0 || !strings.Contains(strings.Join(result.Warnings, ";"), path) {
		t.Fatalf("replacement not named: %+v", result)
	}
}

func TestAuditContainedKeeperAndRetirement(t *testing.T) {
	root := testjail.Scratch(t)
	target := filepath.Join(root, "keeper.toml")
	link := filepath.Join(root, "config.toml")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got := reconcile.Run([]artifact.Artifact{{Kind: artifact.Region, Path: link, Content: []byte("new")}}, reconcile.Build, []artifact.Root{{Path: root}})
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "new" || len(got.Problems) > 0 {
		t.Fatalf("contained keeper: %#v %s %v", got, raw, err)
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "hand.md")
	if err := os.Symlink(target, foreign); err != nil {
		t.Fatal(err)
	}
	retired := reconcile.RetireOwned(foreign, reconcile.Build, root)
	if retired.Deleted != 0 {
		t.Fatalf("foreign alias retired: %#v", retired)
	}
	if _, err = os.Readlink(foreign); err != nil {
		t.Fatal(err)
	}
}

func TestAuditDeletionNamesOwnedLeaves(t *testing.T) {
	root := testjail.Scratch(t)
	dir := filepath.Join(root, "old")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(dir, "SKILL.md")
	foreign := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(owned, []byte(artifact.Marker(artifact.Markdown, "source")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("hand"), 0600); err != nil {
		t.Fatal(err)
	}
	got := reconcile.Run(nil, reconcile.Build, []artifact.Root{{Path: root}})
	named := false
	for _, a := range got.Actions {
		if a.Kind == "delete" && a.Path == owned {
			named = true
		}
		if a.Path == foreign {
			t.Errorf("foreign deletion: %#v", a)
		}
	}
	if !named {
		t.Errorf("owned deletion unnamed: %#v", got)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal(err)
	}
}
