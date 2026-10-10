package machine

import (
	"errors"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestReadGlobalRoleFileRefusesASymlinkTheWayCodexDoes pins the assumption
// this whole change rests on: Codex opens a role with O_NOFOLLOW, so a symlink
// is not readable as a role even when its target is a perfectly good TOML. A
// reader that quietly followed the link would certify a role every spawn
// rejects — and the three outcomes stay distinct, because "Codex will not load
// this", "nothing is here" and "the bytes are these" are three different
// findings.
func TestReadGlobalRoleFileRefusesASymlinkTheWayCodexDoes(t *testing.T) {
	dir := testjail.Scratch(t)
	plain := filepath.Join(dir, "real.toml")
	writeAgentFixture(t, plain, "name = \"real\"\n")
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(plain, link); err != nil {
		t.Fatal(err)
	}

	got, err := ReadGlobalRoleFile(plain)
	if err != nil {
		t.Fatalf("ReadGlobalRoleFile(regular file): %v", err)
	}
	if string(got) != "name = \"real\"\n" {
		t.Fatalf("read %q, want the file's own bytes", got)
	}

	if _, err := ReadGlobalRoleFile(link); !errors.Is(err, ErrGlobalRoleNotRegular) {
		t.Fatalf("ReadGlobalRoleFile(symlink) err=%v, want ErrGlobalRoleNotRegular", err)
	}
	if _, err := ReadGlobalRoleFile(dir); !errors.Is(err, ErrGlobalRoleNotRegular) {
		t.Fatalf("ReadGlobalRoleFile(directory) err=%v, want ErrGlobalRoleNotRegular", err)
	}
	if _, err := ReadGlobalRoleFile(filepath.Join(dir, "absent.toml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadGlobalRoleFile(absent) err=%v, want fs.ErrNotExist — absence is not a refusal", err)
	}
}
