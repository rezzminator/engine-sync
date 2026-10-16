package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPreparationProtectsForeignFileFromMergeCallback(t *testing.T) {
	path := filepath.Join(testjail.Scratch(t), "config.jsonc")
	if err := os.WriteFile(path, []byte("user-owned"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	out, _, problems := reconcile.Prepare([]artifact.Artifact{{Path: path, Merge: func([]byte) ([]byte, []string, []string) { calls++; return []byte("merged"), nil, nil }}})
	if calls != 0 || len(problems) != 0 || len(out) != 1 {
		t.Fatalf("calls=%d %+v", calls, problems)
	}
}

func TestInspectReadsRegularFilesAndFileLinks(t *testing.T) {
	root := testjail.Scratch(t)
	regular := filepath.Join(root, "regular.toml")
	if err := os.WriteFile(regular, []byte("model = 'sample'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "ipc.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"file-link": regular, "socket-link": socket, "device-link": "/dev/null", "directory-link": root, "dangling-link": filepath.Join(root, "missing")} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	var outputs []artifact.Artifact
	var problems []string
	done := make(chan struct{})
	go func() {
		outputs, problems = reconcile.Inspect([]artifact.Root{{Path: root, ValidateOnly: true}}, []artifact.Artifact{{Path: socket}, {Path: "/dev/null"}, {Path: regular, NativeMode: "subagent"}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("inspection blocked opening a FIFO")
	}
	if len(problems) != 0 || len(outputs) != 2 {
		t.Fatalf("outputs=%+v problems=%v", outputs, problems)
	}
	for _, a := range outputs {
		if string(a.Content) != "model = 'sample'\n" || (a.Path == regular && a.NativeMode != "subagent") {
			t.Errorf("regular file inspection lost content or mode: %+v", a)
		}
	}
}

func TestInspectNamesReadAndWalkErrors(t *testing.T) {
	root := testjail.Scratch(t)
	path := filepath.Join(root, "loop")
	if err := os.Symlink(path, path); err != nil {
		t.Fatal(err)
	}
	_, problems := reconcile.Inspect(nil, []artifact.Artifact{{Path: path}})
	if len(problems) != 1 || !strings.Contains(problems[0], "read "+path) {
		t.Fatalf("read error not named: %v", problems)
	}
	// A regular file past the read bound fails the same way for every uid; a permission
	// bit cannot stand in for it because root ignores the bit.
	path = filepath.Join(root, "oversize.toml")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, paths.MaxSourceBytes+1); err != nil {
		t.Fatal(err)
	}
	_, problems = reconcile.Inspect(nil, []artifact.Artifact{{Path: path}})
	if len(problems) != 1 || !strings.Contains(problems[0], "read "+path) || !strings.Contains(problems[0], "byte limit") {
		t.Fatalf("oversize read error not named: %v", problems)
	}
}

// TestInspectNamesUnreadableEntries needs a file and a directory this process
// cannot open, which root can never be denied: as root the test re-runs itself
// as an unprivileged user.
func TestInspectNamesUnreadableEntries(t *testing.T) {
	if testjail.RanUnprivileged(t) {
		return
	}
	root := testjail.Scratch(t)
	path := filepath.Join(root, "unreadable.toml")
	if err := os.WriteFile(path, []byte("model = 'sample'\n"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "unreadable.toml"), 0600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Fatal("fixture does not deny the read")
	}
	_, problems := reconcile.Inspect(nil, []artifact.Artifact{{Path: path}})
	if len(problems) != 1 || !strings.Contains(problems[0], "read "+path) || !strings.Contains(problems[0], "open "+path) {
		t.Fatalf("regular-file open error not named: %v", problems)
	}
	path = filepath.Join(root, "blocked")
	if err := os.Mkdir(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0700) })
	if _, err := os.ReadDir(path); err == nil {
		t.Fatal("fixture does not deny the listing")
	}
	_, problems = reconcile.Inspect([]artifact.Root{{Path: path}}, nil)
	if len(problems) != 1 || !strings.Contains(problems[0], "inspect "+path) {
		t.Fatalf("walk error not named: %v", problems)
	}
	child := filepath.Join(path, "config.toml")
	_, problems = reconcile.Inspect([]artifact.Root{{Path: child}}, nil)
	if len(problems) != 1 || (!strings.Contains(problems[0], child) || !strings.Contains(problems[0], "permission denied")) {
		t.Fatalf("root lstat error not named: %v", problems)
	}
	walked := filepath.Join(root, "walked")
	later := filepath.Join(walked, "later.toml")
	if err := os.MkdirAll(walked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(later, []byte("model = 'sample'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(walked, "blocked")
	if err := os.Mkdir(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0700) })
	outputs, problems := reconcile.Inspect([]artifact.Root{{Path: walked}}, []artifact.Artifact{{Path: later}})
	if len(problems) != 1 || !strings.Contains(problems[0], "inspect "+path) || len(outputs) != 1 || outputs[0].Path != later {
		t.Fatalf("an unreadable directory stopped the walk: outputs=%+v problems=%v", outputs, problems)
	}
}
