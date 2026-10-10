package reconcile

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/paths"
	"io/fs"
	"os"
)

type GlobalRoleInstalled struct {
	Path  string
	State GlobalRoleState
	Found string
}

func GeneratedGlobalRole(content []byte) bool { return artifact.Claims(string(content)) }

// ErrGlobalRoleNotRegular is what ReadGlobalRoleFile returns for a path Codex
// itself would refuse: a symlink, a directory, a device. It is distinct from
// fs.ErrNotExist on purpose — "Codex will not load this" and "there is nothing
// here" are different findings and must never render as one.
var ErrGlobalRoleNotRegular = errors.New("not a regular file — Codex refuses to load it")

// ReadGlobalRoleFile opens path exactly the way Codex's role loader does:
// O_NOFOLLOW on the final component, then a regular-file check on the open
// descriptor. A caller that used os.ReadFile instead would follow the symlink,
// read the bytes Codex never sees, and certify a role that cannot spawn.
func ReadGlobalRoleFile(path string) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, e)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("inspect %s: %w", path, ErrGlobalRoleNotRegular)
	}
	return paths.ReadRegular(path)
}

// GlobalRoleState is the on-disk shape found at one desired role file,
// classified against the bytes this binary compiles for it.
type GlobalRoleState = State

const (
	// GlobalRoleMissing: nothing exists at the target yet.
	GlobalRoleMissing GlobalRoleState = Missing
	// GlobalRoleCurrent: an engine-sync-written regular file whose bytes already match.
	GlobalRoleCurrent GlobalRoleState = State("")
	// GlobalRoleStale: an engine-sync-written regular file whose bytes differ — ours to
	// rewrite.
	GlobalRoleStale GlobalRoleState = Stale
	// GlobalRoleOwnedLink: a symlink engine-sync itself installed, back when roles
	// were linked into the registry. Ours to replace with the real file; this
	// is the migration every host that ran the symlinking installer needs.
	GlobalRoleOwnedLink GlobalRoleState = Stale
	// GlobalRoleForeign: a regular file with no generated marker, a symlink
	// pointing somewhere engine-sync never wrote, or an entry of another type
	// entirely. Never ours — reported, never overwritten, never deleted.
	GlobalRoleForeign GlobalRoleState = Conflict
)

// ClassifyGlobalRole inspects target without mutating the filesystem. found
// carries a symlink's resolved target and is empty otherwise. A stat or read
// failure other than "nothing there" comes back as a non-nil error: an
// unreadable target is "we failed to look", never GlobalRoleMissing.
func ClassifyGlobalRole(target string, want []byte, ownedLinkDirs []string) (GlobalRoleState, string, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return GlobalRoleMissing, "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("inspect %s: %w", target, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		raw, readErr := os.Readlink(target)
		if readErr != nil {
			return "", "", fmt.Errorf("read symlink %s: %w", target, readErr)
		}
		resolved := ResolveLink(target, raw)
		owned, e := Claimable(target, nil)
		if e != nil {
			return "", "", e
		}
		if owned {
			return GlobalRoleOwnedLink, resolved, nil
		}
		return GlobalRoleForeign, resolved, nil
	}
	if !info.Mode().IsRegular() {
		return GlobalRoleForeign, "", nil
	}
	found, err := ReadGlobalRoleFile(target)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", target, err)
	}
	if !GeneratedGlobalRole(found) {
		return GlobalRoleForeign, "", nil
	}
	if bytes.Equal(found, want) {
		return GlobalRoleCurrent, "", nil
	}
	return GlobalRoleStale, "", nil
}

// Describe renders the one operator-facing line every caller shows for a
// classified role, so install's transcript and the CLI can never word the same
// drift two ways.
func (role GlobalRoleInstalled) Describe() string {
	state, target, found := role.State, role.Path, role.Found
	switch state {
	case GlobalRoleMissing, GlobalRoleStale:
		if found != "" {
			return "replace symlink " + target + " -> " + found + " with the role file Codex can load"
		}
		return "write " + target
	case GlobalRoleCurrent:
		return target
	case GlobalRoleForeign:
		if found == "" {
			return "CONFLICT " + target + ": not ours (no engine-sync generated marker); preserved"
		}
		return "CONFLICT " + target + ": not ours (points to " + found + "); preserved"
	default:
		return target
	}
}
