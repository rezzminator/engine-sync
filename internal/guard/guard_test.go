package guard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rezzminator/engine-sync/internal/testjail"
)

func TestMain(m *testing.M) { os.Exit(testjail.RunTests(m)) }

func TestNewerOrdersReleasesAndRefusesNothingItCannotParse(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.9", true},
		{"1.0.0", "0.9.9", true},
		{"0.1.10", "0.1.9", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.1.0-alpha", true},
		{"0.1.0-alpha", "0.1.0", false},
		{"0.1.0-beta", "0.1.0-alpha", true},
		{"0.1.0", "0.2.0", false},
		{"dev", "0.1.0", false},
		{"0.1.0", "dev", false},
		{"v0.2.0", "0.1.0", true},
		{"0.2.0-alpha.10", "0.2.0-alpha.9", true},
		{"0.2.0-alpha.9", "0.2.0-alpha.10", false},
		{"0.2.0-alpha.1", "0.2.0-alpha", true},
		{"0.2.0-beta", "0.2.0-alpha.9", true},
	}
	for _, tc := range cases {
		if got := Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAnOlderWriterIsRefusedAndTheRecordOnlyMovesForward(t *testing.T) {
	state, root := testjail.Scratch(t), "/repo"
	if err := Check(state, root, "0.1.0"); err != nil {
		t.Fatalf("no record must admit the write: %v", err)
	}
	if err := Save(state, root, "0.2.0", "/bin/new"); err != nil {
		t.Fatal(err)
	}
	err := Check(state, root, "0.1.0")
	var newer *NewerWriterError
	if !errors.Is(err, ErrNewerWriter) || !errors.As(err, &newer) || !strings.HasPrefix(err.Error(), "NEWER-WRITER 0.2.0 ") {
		t.Fatalf("older writer: err=%v, want NEWER-WRITER 0.2.0", err)
	}
	if err := Check(state, root, "0.2.0"); err != nil {
		t.Fatalf("the same version must write: %v", err)
	}
	if err := Check(state, "/other", "0.1.0"); err != nil {
		t.Fatalf("another root's record must not refuse: %v", err)
	}
	if err := Save(state, root, "0.1.0", "/bin/old"); err != nil {
		t.Fatal(err)
	}
	if err := Check(state, root, "0.1.0"); !errors.Is(err, ErrNewerWriter) {
		t.Fatalf("after an older writer's save, its check = %v, want NEWER-WRITER: the record moved backward", err)
	}
}

func TestAnUnreadableRecordIsAnErrorNotAPass(t *testing.T) {
	state, root := testjail.Scratch(t), "/repo"
	if err := Save(state, root, "0.1.0", "/bin/x"); err != nil {
		t.Fatal(err)
	}
	path := state + "/writers/" + key(root) + ".json"
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Check(state, root, "0.1.0"); err == nil || !strings.Contains(err.Error(), "parse writer record") {
		t.Fatalf("err=%v, want a parse failure", err)
	}
}

func TestLockIsHeldUntilReleased(t *testing.T) {
	state := testjail.Scratch(t)
	release, err := Lock(state, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	probe := func() error {
		file, err := os.OpenFile(state+"/locks/"+key("/repo")+".lock", os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err := probe(); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("while held, a second writer's try = %v, want EWOULDBLOCK", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := probe(); err != nil {
		t.Fatalf("after release, a second writer's try = %v, want the lock", err)
	}
}

func TestReviewDirtyLockIgnoresWorkingDirectory(t *testing.T) {
	root, state, cwd := testjail.Scratch(t), testjail.Scratch(t), testjail.Scratch(t)
	// A literal namespace-like directory must not become part of path resolution.
	if err := os.Symlink(root, filepath.Join(cwd, "dirty:")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	first, err := CanonicalRoot("dirty:" + root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := Lock(state, "dirty:"+root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	t.Chdir(testjail.Scratch(t))
	second, err := CanonicalRoot("dirty:" + root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || second != "dirty:"+root {
		t.Errorf("dirty lock changed with cwd: %q / %q, want %q", first, second, "dirty:"+root)
	}
	other, err := Lock(state, "dirty:"+root, 20*time.Millisecond)
	if err == nil {
		if err := other(); err != nil {
			t.Error(err)
		}
		t.Fatal("second cwd bypassed held dirty lock")
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("second lock: %v, want ErrBusy", err)
	}
}

func TestReviewCanonicalWriterRoots(t *testing.T) {
	root, state := testjail.Scratch(t), testjail.Scratch(t)
	alias := filepath.Join(testjail.Scratch(t), "alias")
	if e := os.Symlink(root, alias); e != nil {
		t.Fatal(e)
	}
	if e := Save(state, alias, "0.2.0-alpha", "writer"); e != nil {
		t.Fatal(e)
	}
	if e := Check(state, root, "0.1.0"); !errors.Is(e, ErrNewerWriter) {
		t.Fatalf("alias bypassed guard: %v", e)
	}
}

func TestAuditLockAliasesAndUnicode(t *testing.T) {
	base, state := testjail.Scratch(t), testjail.Scratch(t)
	root := filepath.Join(base, "caf\u00e9")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	release, err := Lock(state, root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, spelling := range []string{alias, filepath.Join(base, "cafe\u0301")} {
		unlock, err := Lock(state, spelling, 30*time.Millisecond)
		if unlock != nil {
			_ = unlock()
		}
		if !errors.Is(err, ErrBusy) {
			t.Errorf("lock spelling bypass %s %v", spelling, err)
		}
	}
	if err := Save(state, root, "0.2.0-alpha", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := Check(state, filepath.Join(base, "cafe\u0301"), "0.1.0"); !errors.Is(err, ErrNewerWriter) {
		t.Fatal(err)
	}
}
