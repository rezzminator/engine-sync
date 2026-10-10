package hook

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
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
