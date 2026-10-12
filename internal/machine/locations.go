package machine

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/target/opencode"
	"path/filepath"
	"time"
)

// GlobalRoot is the guard root of a home's machine-level outputs: its Codex
// roles, Claude agent links and global commands.
func GlobalRoot(home string) string { return "global:" + home }

// GuardRoots are the writer-guard roots of every machine registry a global
// run may write: the home's global commands, OpenCode commands and agents,
// each Codex home, and each Claude config dir's agents.
func GuardRoots(home string, codexHomes, claudeConfigDirs []string) []string {
	roots := []string{GlobalRoot(home), GlobalRoot(filepath.Join(home, ".config", "opencode", "command")), GlobalRoot(opencode.MachineAgentsDir(home))}
	for _, h := range codexHomes {
		roots = append(roots, GlobalRoot(h))
	}
	for _, d := range claudeConfigDirs {
		roots = append(roots, GlobalRoot(filepath.Join(d, "agents")))
	}
	return roots
}

// AdoptDir is where one `global --adopt` run moves the unowned files it
// replaces: {state}/adopted/{UTC timestamp}.
func AdoptDir(state string, now time.Time) string {
	return filepath.Join(state, "adopted", now.UTC().Format("20060102T150405Z"))
}

type Mode uint8

const (
	ModeBuild Mode = iota
	ModeCheck
	actionWrite = "write"
	actionLink  = "link"
)

func resolveMachineHome(home string) (string, error) {
	if home == "" {
		var e error
		home, e = paths.Home()
		if e != nil {
			return "", e
		}
	}
	p, e := filepath.Abs(home)
	if e != nil {
		return "", fmt.Errorf("resolve home: %w", e)
	}
	return filepath.Clean(p), nil
}
