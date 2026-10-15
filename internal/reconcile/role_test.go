package reconcile_test

import (
	"errors"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestMachineRoleRepairPublishesRegularFile(t *testing.T) {
	path := filepath.Join(testjail.Scratch(t), "role.toml")
	content := []byte(artifact.Marker(artifact.TOML, "source") + "\nname = \"reader\"\n")
	state, _, err := reconcile.ClassifyGlobalRole(path, content, nil)
	if err != nil || state != reconcile.GlobalRoleMissing {
		t.Fatal(state, err)
	}
	result := reconcile.Run([]artifact.Artifact{{Path: path, Content: content}}, reconcile.Build, []artifact.Root{{Path: filepath.Dir(path)}})
	if len(result.Problems) > 0 {
		t.Fatal(result.Problems)
	}
	got, err := reconcile.ReadGlobalRoleFile(path)
	if err != nil || string(got) != string(content) {
		t.Fatalf("%q %v", got, err)
	}
}

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
	if err := os.WriteFile(plain, []byte("name = \"real\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(plain, link); err != nil {
		t.Fatal(err)
	}

	got, err := reconcile.ReadGlobalRoleFile(plain)
	if err != nil {
		t.Fatalf("ReadGlobalRoleFile(regular file): %v", err)
	}
	if string(got) != "name = \"real\"\n" {
		t.Fatalf("read %q, want the file's own bytes", got)
	}

	if _, err := reconcile.ReadGlobalRoleFile(link); !errors.Is(err, reconcile.ErrGlobalRoleNotRegular) {
		t.Fatalf("ReadGlobalRoleFile(symlink) err=%v, want ErrGlobalRoleNotRegular", err)
	}
	if _, err := reconcile.ReadGlobalRoleFile(dir); !errors.Is(err, reconcile.ErrGlobalRoleNotRegular) {
		t.Fatalf("ReadGlobalRoleFile(directory) err=%v, want ErrGlobalRoleNotRegular", err)
	}
	if _, err := reconcile.ReadGlobalRoleFile(filepath.Join(dir, "absent.toml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadGlobalRoleFile(absent) err=%v, want fs.ErrNotExist — absence is not a refusal", err)
	}
}
