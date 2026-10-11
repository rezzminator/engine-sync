package hook

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(testjail.RunTests(m)) }
func TestDirtyFlagsDistinguishSameNamedProjects(t *testing.T) {
	home := testjail.Scratch(t)
	e := Context{Home: func() (string, error) { return home, nil }, State: func(string) string { return filepath.Join(home, "state") }}
	a, err := DirtyFlag(e, filepath.Join(home, "one", "repo"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := DirtyFlag(e, filepath.Join(home, "two", "repo"))
	if err != nil || a == b || filepath.Dir(a) != filepath.Dir(b) {
		t.Fatalf("%s %s %v", a, b, err)
	}
}

func TestReviewStopRetainsFlagDuringStages(t *testing.T) {
	home := testjail.Scratch(t)
	root := filepath.Join(home, "project")
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "engine-sync.json"), []byte(`{"version":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	e := Context{Home: func() (string, error) { return home, nil }, Get: func(key string) string {
		if key == "CLAUDE_PROJECT_DIR" {
			return root
		}
		return ""
	}, State: func(string) string { return filepath.Join(home, "state") }, Policy: func(string, string, io.Writer) (*config.Config, error) { c := config.Default(); return &c, nil }, Writer: func() string { return "/fixture/bin/engine-sync" }, Engines: []string{"sample"}}
	payload := Payload{}
	payload.ToolInput.FilePath = filepath.Join(root, "CLAUDE.md")
	var out, stderr bytes.Buffer
	Mark(payload, &stderr, e)
	flag, err := DirtyFlag(e, root)
	if err != nil {
		t.Fatal(err)
	}
	marked := false
	Stop(Payload{}, &out, &stderr, e, func([]string) (int, string) {
		if _, err := os.Lstat(flag); err != nil {
			t.Errorf("kill during this stage loses dirty flag: %v", err)
		}
		if !marked {
			Mark(payload, &stderr, e)
			marked = true
		}
		return 0, ""
	})
	if _, err := os.Lstat(flag); err != nil {
		t.Fatalf("concurrent mark was erased: %v output=%s error=%s", err, &out, &stderr)
	}
}
