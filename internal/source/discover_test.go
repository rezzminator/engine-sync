package source

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMarkdownDiscoverySortsAndExcludesSubtrees(t *testing.T) {
	root := testjail.Scratch(t)
	for _, name := range []string{"b.md", "a.md", "README.md", "skip/c.md"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("body"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	fail := func(s string) { t.Fatal(s) }
	got := Markdown(root, []string{"skip"}, Reporter{Problem: fail, Warning: fail, Dangling: func(p, r string, e error) { t.Fatal(e) }})
	if len(got) != 2 || got[0].Rel != "a.md" || got[1].Rel != "b.md" {
		t.Fatalf("%+v", got)
	}
}

func TestReviewCachedExcludedSources(t *testing.T) {
	root := testjail.Scratch(t)
	dir := filepath.Join(root, ".claude", "commands", "a", "drafts")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("missing", filepath.Join(dir, "bad.md")); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "x.md"), []byte("body"), 0600); e != nil {
		t.Fatal(e)
	}
	cfg := config.Default()
	cfg.Codex.ExcludeDirs = []string{"drafts"}
	p := LoadProject(root, testjail.Scratch(t), cfg)
	if len(p.Commands) != 0 || len(p.Unresolved) != 0 || len(p.Problems) != 0 {
		t.Fatalf("excluded source leaked: %+v", p)
	}
}

func TestReviewSourceForms(t *testing.T) {
	root := testjail.Scratch(t)
	fail := func(s string) { t.Error(s) }
	report := Reporter{Problem: fail, Warning: fail, Dangling: func(p, r string, e error) { t.Error(e) }}
	for _, name := range []string{"real/x.md", "README.md"} {
		path := filepath.Join(root, name)
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, []byte("body"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.Symlink("real", filepath.Join(root, "alias")); e != nil {
		t.Fatal(e)
	}
	if got := Markdown(root, nil, report); len(got) != 2 {
		t.Errorf("aliases lost: %+v", got)
	}
	problems := []string{}
	report.Problem = func(s string) { problems = append(problems, s) }
	Projects(root, []string{"missing"}, nil, report)
	if len(problems) == 0 {
		t.Error("listed missing project silently lost")
	}
	empty := testjail.Scratch(t)
	if e := os.WriteFile(filepath.Join(empty, "README.md"), []byte("help"), 0600); e != nil {
		t.Fatal(e)
	}
	if p, e := LoadMachine(empty, empty, filepath.Join(empty, "generated")); e != nil || len(p.Agents) != 0 {
		t.Fatalf("ordinary source: agents=%d error=%v", len(p.Agents), e)
	}
}
func TestReviewNonregularSource(t *testing.T) {
	root := testjail.Scratch(t)
	pipe := filepath.Join(root, "pipe.md")
	if e := syscall.Mkfifo(pipe, 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { p := &Project{}; _, e := p.Read(pipe); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Error("special source accepted")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("special source blocks read")
	}
}
