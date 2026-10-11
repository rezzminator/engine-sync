package reconcile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rezzminator/engine-sync/internal/paths"
)

// GlobalLinkState is the on-disk shape check found at one desired global
// registry target, classified against its desired symlink source. Every
// caller — agents, commands, skills — shares this one vocabulary so a drift
// never reads differently depending on which registry found it.
type GlobalLinkState = State

const (
	// GlobalLinkMissing: nothing exists at the target yet.
	GlobalLinkMissing GlobalLinkState = Missing
	// GlobalLinkCorrect: a symlink already resolves to the desired source.
	GlobalLinkCorrect GlobalLinkState = State("")
	// GlobalLinkCopy: a plain file or directory sits at the target — the
	// shape the old copy-based installer left behind, or one just like it.
	// Its basename is the roster entry itself (that is what put a target
	// here at all), so it is ours to replace with a link.
	GlobalLinkCopy GlobalLinkState = Stale
	// GlobalLinkWrongTarget: a symlink exists but resolves somewhere else
	// INSIDE the source repository — stale (a rename, a re-rostered file),
	// still ours, safe to repoint.
	GlobalLinkWrongTarget GlobalLinkState = Stale
	// GlobalLinkConflict: a symlink resolving outside the source repository,
	// or a directory sitting where a file link belongs (or vice versa).
	// Never ours — reported, never overwritten, never deleted.
	GlobalLinkConflict GlobalLinkState = Conflict
	// GlobalLinkDangling: a symlink already points at the desired source
	// path, but nothing exists there anymore — the engine-sync-owned generated
	// directory a compiled Codex agent twin lives in was removed (partial
	// uninstall, a wiped state dir) while the registry link survived. Ours,
	// but a build cannot silently call this "correct": the link resolving to
	// the right NAME is not the same as an operator actually having the
	// agent. Reconciliation reports it as unresolvable —
	// the recompile step that runs first in RunGlobalAgents' build phase
	// recreates the missing file before this link is re-applied.
	GlobalLinkDangling GlobalLinkState = Dangling
)

// GlobalLinkKind selects whether the desired target is a single file link or
// a whole-directory link. It changes exactly one thing: whether an existing
// plain entry of the OTHER shape counts as "ours" (a copy) or as a conflict.
type GlobalLinkKind int

const (
	GlobalLinkFile GlobalLinkKind = iota
	GlobalLinkDir
)

// ClassifyGlobalLink inspects target without mutating the filesystem. A
// genuine stat/read failure (permission denied, a symlink loop — anything
// other than "nothing there") comes back as a non-nil error: an unreadable
// target is "we failed to look", never "0 entries", and must never be read
// as GlobalLinkMissing.
func ClassifyGlobalLink(target, source, sourceRepoRoot string, kind GlobalLinkKind) (GlobalLinkState, string, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return GlobalLinkMissing, "", nil
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
		if resolved == filepath.Clean(source) {
			if _, statErr := os.Stat(resolved); statErr != nil {
				if errors.Is(statErr, fs.ErrNotExist) {
					return GlobalLinkDangling, resolved, nil
				}
				return "", "", fmt.Errorf("inspect global link target %s: %w", resolved, statErr)
			}
			return GlobalLinkCorrect, resolved, nil
		}
		owned, e := Claimable(target, nil)
		if e != nil {
			return "", "", e
		}
		if owned {
			return GlobalLinkWrongTarget, resolved, nil
		}
		return GlobalLinkConflict, resolved, nil
	}
	wantDir := kind == GlobalLinkDir
	if info.IsDir() == wantDir {
		owned, e := Claimable(target, nil)
		if e != nil {
			return "", "", e
		}
		if owned {
			return GlobalLinkCopy, "", nil
		}
	}
	return GlobalLinkConflict, "", nil
}

// DescribeGlobalLinkState renders the one operator-facing line every caller
// shows for a classified state, so install's transcript, its check preview,
// and the engine-sync codex agents CLI never describe the same drift differently.
func DescribeGlobalLinkState(state GlobalLinkState, target, source, found string) string {
	switch state {
	case GlobalLinkMissing:
		return "link " + target + " -> " + source
	case GlobalLinkCorrect:
		return target
	case GlobalLinkCopy:
		if found != "" {
			return "repoint " + target + " -> " + source + " (was " + found + ")"
		}
		return "replace copy " + target + " -> " + source
	case GlobalLinkDangling:
		return string(Dangling) + " " + target + " -> " + found + " (target does not exist — rerun engine-sync global)"
	case GlobalLinkConflict:
		if found == "" {
			return "CONFLICT " + target + ": not ours"
		}
		return "CONFLICT " + target + ": not ours (points to " + found + ")"
	default:
		return target
	}
}

func ResolveLink(path, raw string) string {
	target := raw
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}

func WithinLinkRoot(candidate, root string) (bool, error) {
	var e error
	candidate, e = paths.PhysicalPath(candidate)
	if e != nil {
		return false, e
	}
	root, e = paths.PhysicalPath(root)
	if e != nil {
		return false, e
	}
	return candidate == root || strings.HasPrefix(candidate, root+string(filepath.Separator)), nil
}
