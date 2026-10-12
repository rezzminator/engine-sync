package machine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/atomicfile"
	"github.com/rezzminator/engine-sync/internal/paths"
)

// planAgents is the agents plan. With --adopt every unowned regular file at
// a declared agent's path moves aside first, and the plan is taken again so
// the build replaces it; under --plan nothing moves: each such file is a
// planned adoption, and its conflict leaves the plan.
func (r *run) planAgents(agents GlobalAgentsOptions) (GlobalAgentsResult, error) {
	plan, err := RunGlobalAgents(agents)
	if err != nil || r.options.AdoptDir == "" {
		return plan, wrapPlanError(err)
	}
	adoptions, err := r.adoptable(r.options.AdoptDir)
	if err != nil || len(adoptions) == 0 {
		return plan, err
	}
	if r.options.Plan {
		plan.Problems = dropAdopted(plan.Problems, adoptions)
		for _, a := range adoptions {
			r.add(VerbChange, "adopt "+a.from+" -> "+a.to, "")
		}
		return plan, nil
	}
	for _, a := range adoptions {
		if err := r.adoptOne(a); err != nil {
			return plan, err
		}
	}
	plan, err = RunGlobalAgents(agents)
	return plan, wrapPlanError(err)
}

func wrapPlanError(err error) error {
	if err != nil {
		return fmt.Errorf("plan engine-sync codex agents: %w", err)
	}
	return nil
}

// adoption is one unowned regular file at a declared agent's path and where
// --adopt moves it.
type adoption struct{ from, to string }

// adoptable lists every unowned regular file at a declared agent's path with
// its destination adoptDir/{path relative to home}. A SHADOWED link or
// directory is never adopted, nor a file under a symlinked parent — a tree
// the build refuses to write — and the same file listed twice is one move.
func (r *run) adoptable(adoptDir string) ([]adoption, error) {
	// An agent that failed to render is named by the build and never
	// adopted; the states of the others still classify.
	states, err := AgentStates(r.options.Home, r.options.SourceRepo, &r.policy, r.claudeConfigDirs(), r.codexHomes())
	if err != nil && !errors.Is(err, errRenderFailures) {
		return nil, fmt.Errorf("classify agents to adopt: %w", err)
	}
	var out []adoption
	seen, taken := map[string]bool{}, map[string]bool{}
	for _, s := range states {
		if s.State != StateShadowed {
			continue
		}
		info, err := os.Lstat(s.Path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s to adopt: %w", s.Path, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		linked, err := symlinkedParent(r.options.Home, append(r.codexHomes(), r.claudeConfigDirs()...), s.Path)
		if err != nil {
			return nil, err
		}
		if linked != "" {
			r.skip("SHADOWED " + s.Path + " not adopted: parent " + linked + " is a symlink")
			continue
		}
		physical, err := paths.PhysicalPath(s.Path)
		if err != nil {
			return nil, fmt.Errorf("resolve %s to adopt: %w", s.Path, err)
		}
		if seen[physical] {
			continue
		}
		seen[physical] = true
		rel, err := filepath.Rel(r.options.Home, s.Path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			rel = strings.TrimPrefix(filepath.Clean(s.Path), string(filepath.Separator))
		}
		to, err := freeDestination(filepath.Join(adoptDir, rel), taken)
		if err != nil {
			return nil, err
		}
		taken[to] = true
		out = append(out, adoption{from: s.Path, to: to})
	}
	return out, nil
}

// symlinkedParent names the first symlinked directory from path's directory
// up to its boundary — the Codex home or Claude config dir holding it, itself
// included, else home, itself excluded — the parents the build refuses to
// write under (reconcile's safeParents). Empty means every one is real.
func symlinkedParent(home string, bases []string, path string) (string, error) {
	boundary, inclusive := filepath.Clean(home), false
	for _, base := range bases {
		if within(path, filepath.Clean(base)) {
			boundary, inclusive = filepath.Clean(base), true
			break
		}
	}
	for p := filepath.Dir(path); within(p, boundary); p = filepath.Dir(p) {
		if p == boundary && !inclusive {
			return "", nil
		}
		info, err := os.Lstat(p)
		if err != nil {
			return "", fmt.Errorf("inspect %s to adopt: %w", p, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return p, nil
		}
		if p == boundary || p == filepath.Dir(p) {
			break
		}
	}
	return "", nil
}

// dropAdopted removes the conflicts a planned adoption resolves.
func dropAdopted(problems []string, adoptions []adoption) []string {
	kept := problems[:0:0]
	for _, problem := range problems {
		resolved := false
		for _, a := range adoptions {
			if strings.Contains(problem, a.from) {
				resolved = true
			}
		}
		if !resolved {
			kept = append(kept, problem)
		}
	}
	return kept
}

// freeDestination is the first of to, {stem}.1{ext}, {stem}.2{ext}, … that
// neither exists nor is taken by an earlier adoption of this run, so an
// adoption never replaces a file adopted before it.
func freeDestination(to string, taken map[string]bool) (string, error) {
	ext := filepath.Ext(to)
	stem := strings.TrimSuffix(to, ext)
	for n := 0; ; n++ {
		candidate := to
		if n > 0 {
			candidate = fmt.Sprintf("%s.%d%s", stem, n, ext)
		}
		if taken[candidate] {
			continue
		}
		_, err := os.Lstat(candidate)
		if errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect adopt destination %s: %w", candidate, err)
		}
	}
}

// moveFile is atomicfile.Move; a variable so a test can refuse a move.
var moveFile = atomicfile.Move

// adoptOne moves one planned adoption aside. Ownership is checked again right
// before the move: a file gone, no longer regular, or now carrying
// engine-sync's marker stays where it is and is named, as does one whose move
// is refused, and a destination that appeared since the plan gets the next
// free suffix.
func (r *run) adoptOne(a adoption) error {
	info, err := os.Lstat(a.from)
	if errors.Is(err, fs.ErrNotExist) {
		r.skip(a.from + " not adopted: it is gone")
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s to adopt: %w", a.from, err)
	}
	if !info.Mode().IsRegular() {
		r.skip(a.from + " not adopted: no longer a regular file")
		return nil
	}
	raw, err := paths.ReadRegular(a.from)
	if err != nil {
		return fmt.Errorf("read %s to adopt: %w", a.from, err)
	}
	if artifact.Claims(string(raw)) {
		r.skip(a.from + " not adopted: it carries the engine-sync marker")
		return nil
	}
	// A destination another writer created since freeDestination looked
	// takes the next free suffix; any other refused move leaves the file in
	// place, named, and its path stays the CONFLICT it is without --adopt.
	var to string
	for taken := map[string]bool{}; ; taken[to] = true {
		to, err = freeDestination(a.to, taken)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return fmt.Errorf("adopt %s: %w", a.from, err)
		}
		err = moveFile(a.from, to)
		if !errors.Is(err, fs.ErrExist) {
			break
		}
	}
	if err != nil {
		r.skip(fmt.Sprintf("%s not adopted: move to %s: %v", a.from, to, err))
		return nil
	}
	r.add(VerbChange, "adopted "+a.from+" -> "+to, a.from)
	return nil
}
