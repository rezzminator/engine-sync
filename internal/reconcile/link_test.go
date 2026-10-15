package reconcile_test

import (
	"errors"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineLinkRepairsMissingTarget(t *testing.T) {
	root := testjail.Scratch(t)
	source := filepath.Join(root, "source.md")
	if err := os.WriteFile(source, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.md")
	state, _, err := reconcile.ClassifyGlobalLink(target, source, root, reconcile.GlobalLinkFile)
	if err != nil || state != reconcile.GlobalLinkMissing {
		t.Fatal(state, err)
	}
	result := reconcile.Run([]artifact.Artifact{{Path: target, Target: source, Kind: artifact.Link}}, reconcile.Build, []artifact.Root{{Path: root}})
	if len(result.Problems) > 0 {
		t.Fatal(result.Problems)
	}
	state, _, err = reconcile.ClassifyGlobalLink(target, source, root, reconcile.GlobalLinkFile)
	if err != nil || state != reconcile.GlobalLinkCorrect {
		t.Fatal(state, err)
	}
}

func TestClassifyGlobalLinkAliasRoot(t *testing.T) {
	root := testjail.Scratch(t)
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, foundRoot, sourceRoot string }{
		{"alias target", alias, repo}, {"alias root", repo, alias},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := filepath.Join(root, tc.name)
			stale := filepath.Join(tc.foundRoot, "stale.md")
			if err := os.Symlink(stale, target); err != nil {
				t.Fatal(err)
			}
			state, _, err := reconcile.ClassifyGlobalLink(
				target, filepath.Join(tc.sourceRoot, "desired.md"), tc.sourceRoot, reconcile.GlobalLinkFile,
			)
			if err != nil || state != reconcile.GlobalLinkConflict {
				t.Fatalf("state=%s err=%v", state, err)
			}
		})
	}
}

// TestClassifyGlobalLinkStates walks every state ClassifyGlobalLink must
// distinguish for a FILE-kind desired target: missing, an already-correct
// link, a same-shape copy (ours, safe to replace), a symlink stale but still
// inside the source repo (ours, safe to repoint), and a symlink pointing
// entirely outside the source repo (a conflict, never touched).
func TestClassifyGlobalLinkStates(t *testing.T) {
	root := testjail.Scratch(t)
	sourceRepo := filepath.Join(root, "repo")
	source := filepath.Join(sourceRepo, "templates", "global", "agents", "alpha.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("agent body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("missing", func(t *testing.T) {
		target := filepath.Join(root, "missing", "alpha.md")
		state, found, err := reconcile.ClassifyGlobalLink(target, source, sourceRepo, reconcile.GlobalLinkFile)
		if err != nil {
			t.Fatal(err)
		}
		if state != reconcile.GlobalLinkMissing || found != "" {
			t.Fatalf("state=%s found=%q, want missing/\"\"", state, found)
		}
	})

	t.Run("correct", func(t *testing.T) {
		target := filepath.Join(root, "correct", "alpha.md")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(source, target); err != nil {
			t.Fatal(err)
		}
		state, _, err := reconcile.ClassifyGlobalLink(target, source, sourceRepo, reconcile.GlobalLinkFile)
		if err != nil {
			t.Fatal(err)
		}
		if state != reconcile.GlobalLinkCorrect {
			t.Fatalf("state=%s, want correct", state)
		}
	})

	t.Run("copy", func(t *testing.T) {
		target := filepath.Join(root, "copy", "alpha.md")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("agent body\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		state, found, err := reconcile.ClassifyGlobalLink(target, source, sourceRepo, reconcile.GlobalLinkFile)
		if err != nil {
			t.Fatal(err)
		}
		if state != reconcile.GlobalLinkConflict || found != "" {
			t.Fatalf("state=%s found=%q, want copy/\"\"", state, found)
		}
	})

	t.Run("wrong-target still in repo", func(t *testing.T) {
		stale := filepath.Join(sourceRepo, "templates", "global", "agents", "retired.md")
		if err := os.WriteFile(stale, []byte("stale\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "wrong-target", "alpha.md")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(stale, target); err != nil {
			t.Fatal(err)
		}
		state, found, err := reconcile.ClassifyGlobalLink(target, source, sourceRepo, reconcile.GlobalLinkFile)
		if err != nil {
			t.Fatal(err)
		}
		if state != reconcile.GlobalLinkConflict || found != stale {
			t.Fatalf("state=%s found=%q, want wrong-target/%q", state, found, stale)
		}
	})

	t.Run("dangling — link resolves to the desired source, but nothing exists there", func(t *testing.T) {
		goneSource := filepath.Join(sourceRepo, "generated", "gone.toml")
		target := filepath.Join(root, "dangling", "gone.toml")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(goneSource, target); err != nil {
			t.Fatal(err)
		}
		state, found, err := reconcile.ClassifyGlobalLink(target, goneSource, sourceRepo, reconcile.GlobalLinkFile)
		if err != nil {
			t.Fatal(err)
		}
		if state != reconcile.GlobalLinkDangling || found != goneSource {
			t.Fatalf("state=%s found=%q, want dangling/%q", state, found, goneSource)
		}
	})

	t.Run("conflict foreign symlink", func(t *testing.T) {
		elsewhere := filepath.Join(root, "elsewhere.md")
		if err := os.WriteFile(elsewhere, []byte("foreign\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "conflict", "alpha.md")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(elsewhere, target); err != nil {
			t.Fatal(err)
		}
		state, found, err := reconcile.ClassifyGlobalLink(target, source, sourceRepo, reconcile.GlobalLinkFile)
		if err != nil {
			t.Fatal(err)
		}
		if state != reconcile.GlobalLinkConflict || found != elsewhere {
			t.Fatalf("state=%s found=%q, want conflict/%q", state, found, elsewhere)
		}
	})

	t.Run("conflict type mismatch — a directory sitting where a file link belongs", func(t *testing.T) {
		target := filepath.Join(root, "type-mismatch", "alpha.md")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		state, _, err := reconcile.ClassifyGlobalLink(target, source, sourceRepo, reconcile.GlobalLinkFile)
		if err != nil {
			t.Fatal(err)
		}
		if state != reconcile.GlobalLinkConflict {
			t.Fatalf("state=%s, want conflict — a directory must never be silently deleted", state)
		}
	})

	t.Run("dir-kind copy is replaceable, unlike a dir-kind conflict", func(t *testing.T) {
		target := filepath.Join(root, "dir-copy", "pcm")
		if err := os.MkdirAll(filepath.Join(target, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		dirSource := filepath.Join(sourceRepo, "templates", "global", "skills", "pcm")
		state, _, err := reconcile.ClassifyGlobalLink(target, dirSource, sourceRepo, reconcile.GlobalLinkDir)
		if err != nil {
			t.Fatal(err)
		}
		if state != reconcile.GlobalLinkConflict {
			t.Fatalf("state=%s, want copy for a same-shape directory", state)
		}
	})
}

// TestClassifyGlobalLinkUnreadableIsAnErrorNeverMissing pins the rule this
// package exists to enforce: a probe that could not run must never render as
// "nothing there". A parent that is a regular file makes Lstat fail with
// ENOTDIR, which no uid can bypass and which is not ErrNotExist, and that must surface as
// a genuine error, not GlobalLinkMissing.
func TestClassifyGlobalLinkUnreadableIsAnErrorNeverMissing(t *testing.T) {
	root := testjail.Scratch(t)
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(blocked, "alpha.md")
	if _, err := os.Lstat(target); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("fixture does not make the probe fail with something other than absence: %v", err)
	}
	source := filepath.Join(root, "repo", "templates", "global", "agents", "alpha.md")
	_, _, err := reconcile.ClassifyGlobalLink(target, source, filepath.Join(root, "repo"), reconcile.GlobalLinkFile)
	if err == nil {
		t.Fatal(
			"ClassifyGlobalLink reported no error for an unreadable target — an unreadable probe must never read as absence",
		)
	}
}

func TestDescribeGlobalLinkStateNamesConflictExactly(t *testing.T) {
	got := reconcile.DescribeGlobalLinkState(
		reconcile.GlobalLinkConflict,
		"/home/x/.claude/agents/alpha.md",
		"/repo/agents/alpha.md",
		"/home/x/elsewhere.md",
	)
	want := "CONFLICT /home/x/.claude/agents/alpha.md: not ours (points to /home/x/elsewhere.md)"
	if got != want {
		t.Fatalf("DescribeGlobalLinkState = %q, want %q", got, want)
	}
}

func TestDescribeGlobalLinkStateNamesDanglingByLinkName(t *testing.T) {
	got := reconcile.DescribeGlobalLinkState(
		reconcile.GlobalLinkDangling,
		"/home/x/.codex/agents/alpha.toml",
		"/home/x/.local/state/engine-sync/generated/codex-agents/alpha.toml",
		"/home/x/.local/state/engine-sync/generated/codex-agents/alpha.toml",
	)
	if !strings.Contains(got, "DANGLING") || !strings.Contains(got, "/home/x/.codex/agents/alpha.toml") {
		t.Fatalf("DescribeGlobalLinkState(dangling) = %q, want it to name DANGLING and the link path", got)
	}
}
