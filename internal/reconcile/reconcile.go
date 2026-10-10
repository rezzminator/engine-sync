package reconcile

import (
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/atomicfile"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	actionWrite  = "write"
	actionLink   = "link"
	actionDelete = "delete"
	actionChmod  = "chmod"
)

func Run(outputs []artifact.Artifact, mode Mode, roots []artifact.Root, ownership ...func(string) (bool, error)) Result {
	prepared, warnings, problems := Prepare(outputs)
	result := Result{Warnings: warnings, Problems: problems}
	outputs = prepared
	if mode == Build && len(problems) > 0 {
		return result
	}
	managed := make([]string, 0, len(roots))
	for _, root := range roots {
		if !root.ValidateOnly {
			managed = append(managed, root.Path)
		}
	}
	owns := func(path string) (bool, error) { return Claimable(path, roots) }
	if len(ownership) > 0 {
		owns = ownership[0]
	}
	wanted := map[string]bool{}
	real := map[string]bool{}
	for _, output := range outputs {
		if output.Kept == nil {
			real[ManagedEntry(output.Path, managed)] = true
		}
	}
	for _, output := range outputs {
		entry := ManagedEntry(output.Path, managed)
		if output.Kept != nil && real[entry] {
			continue
		}
		wanted[entry] = true
		if output.Kept != nil {
			warning, problem := KeepTwin(output.Path, output.Kept.Path, output.Kept.Target)
			if warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
			if problem != "" {
				result.Problems = append(result.Problems, problem)
			}
			continue
		}
		if output.Kind == artifact.Link {
			LinkArtifact(&result, output, mode, owns)
		} else {
			FileArtifact(&result, output, mode, owns)
		}
	}
	for _, dir := range managed {
		Orphans(&result, dir, wanted, mode, owns)
	}
	return result
}

func ManagedEntry(path string, managed []string) string {
	for _, root := range managed {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return filepath.Join(root, strings.Split(rel, string(filepath.Separator))[0])
	}
	return path
}

func LinkArtifact(result *Result, output artifact.Artifact, mode Mode, owns func(string) (bool, error)) {
	info, err := os.Lstat(output.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		result.Problems = append(result.Problems, unreadableProblem(output.Path, err))
		return
	}
	exists := err == nil
	if exists && info.Mode()&os.ModeSymlink != 0 {
		if link, readErr := os.Readlink(output.Path); readErr == nil && link == output.Target {
			result.Unchanged++
			return
		}
	}
	if exists && output.Kind != artifact.Region {
		if refusal := claimProblem(output, owns); refusal != "" {
			result.Problems = append(result.Problems, refusal)
			return
		}
	}
	if mode != Build {
		problem := fmt.Sprintf("%s %s (want symlink → %s)", outputState(exists), output.Path, output.Target)
		result.Problems = append(result.Problems, problem)
		result.Rebuildable = append(result.Rebuildable, problem)
		result.Actions = append(result.Actions, Action{Kind: actionLink, Path: output.Path, Target: output.Target})
		return
	}
	result.Actions = append(result.Actions, Action{Kind: actionLink, Path: output.Path, Target: output.Target})
	if exists && info.IsDir() {
		if removeErr := os.RemoveAll(output.Path); removeErr != nil {
			result.Problems = append(result.Problems, fmt.Sprintf("remove %s: %v", output.Path, removeErr))
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(output.Path), 0o755); err != nil {
		result.Problems = append(result.Problems, fmt.Sprintf("mkdir %s: %v", output.Path, err))
		return
	}
	if err := atomicfile.Symlink(output.Target, output.Path); err != nil {
		result.Problems = append(result.Problems, fmt.Sprintf("symlink %s: %v", output.Path, err))
		return
	}
	result.Wrote++
}

// outputState names an output check would rewrite: only an absent path is
// MISSING; anything present (empty or not) is STALE.
func outputState(exists bool) string {
	if exists {
		return string(Stale)
	}
	return string(Missing)
}

func unreadableProblem(path string, err error) string {
	return fmt.Sprintf("inspect %s: %v", path, err)
}

// claimProblem is the problem that forbids touching an existing output, or
// "" when engine-sync provably owns it.
func claimProblem(output artifact.Artifact, owns func(string) (bool, error)) string {
	claimable, err := owns(output.Path)
	if err != nil {
		return unreadableProblem(output.Path, err)
	}
	if claimable {
		return ""
	}

	if output.Source != "" && !artifact.Claims(string(output.Content)) {
		return fmt.Sprintf("%s %s — differs from %s and engine-sync cannot prove it wrote it; delete it and rebuild", Conflict, output.Path, output.Source)
	}
	return fmt.Sprintf("CONFLICT %s — exists without a generated marker; not touching it", output.Path)
}

func FileArtifact(result *Result, output artifact.Artifact, mode Mode, owns func(string) (bool, error)) {
	wantMode := output.Mode
	if wantMode == 0 {
		wantMode = 0o644
	}
	info, err := os.Lstat(output.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		result.Problems = append(result.Problems, unreadableProblem(output.Path, err))
		return
	}
	exists := err == nil
	if !exists && output.OptionalEmpty && len(output.Content) == 0 {
		return
	}
	regular := exists && info.Mode().IsRegular()
	current := ""
	haveMode := os.FileMode(0)
	if regular {
		haveMode = info.Mode().Perm()
		raw, readErr := os.ReadFile(output.Path)
		if readErr != nil {
			result.Problems = append(result.Problems, unreadableProblem(output.Path, readErr))
			return
		}
		current = string(raw)
	}
	// A generated file whose content is already right but whose mode drifted
	// (an operator's chmod, a restore from a permission-lossy archive) is not
	// "Unchanged" (L3-F14) — check names it distinctly from STALE/MISSING
	// content, and build fixes it with a chmod rather than rewriting content
	// that was already correct.
	modeOnlyDrift := current == string(output.Content) && regular && haveMode != wantMode
	if current == string(output.Content) && !modeOnlyDrift {
		result.Unchanged++
		return
	}
	if exists && output.Kind != artifact.Region {
		if refusal := claimProblem(output, owns); refusal != "" {
			result.Problems = append(result.Problems, refusal)
			return
		}
	}
	if mode != Build {
		if modeOnlyDrift {
			problem := fmt.Sprintf("%s %s (mode want %04o, have %04o)", Stale, output.Path, wantMode, haveMode)
			result.Problems = append(result.Problems, problem)
			result.Rebuildable = append(result.Rebuildable, problem)
			result.Actions = append(
				result.Actions,
				Action{Kind: actionChmod, Path: output.Path, Target: fmt.Sprintf("%04o", wantMode)},
			)
			return
		}
		problem := fmt.Sprintf("%s %s", outputState(exists), output.Path)
		result.Problems = append(result.Problems, problem)
		result.Rebuildable = append(result.Rebuildable, problem)
		result.Actions = append(result.Actions, Action{Kind: actionWrite, Path: output.Path})
		return
	}
	if modeOnlyDrift {
		result.Actions = append(
			result.Actions,
			Action{Kind: actionChmod, Path: output.Path, Target: fmt.Sprintf("%04o", wantMode)},
		)
		if err := os.Chmod(output.Path, wantMode); err != nil {
			result.Problems = append(result.Problems, fmt.Sprintf("chmod %s: %v", output.Path, err))
			return
		}
		result.Wrote++
		return
	}
	result.Actions = append(result.Actions, Action{Kind: actionWrite, Path: output.Path})
	if err := atomicfile.Write(output.Path, output.Content, wantMode); err != nil {
		result.Problems = append(result.Problems, fmt.Sprintf("write %s: %v", output.Path, err))
		return
	}
	result.Wrote++
}

func Orphans(result *Result, dir string, wanted map[string]bool, mode Mode, owns func(string) (bool, error)) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		result.Problems = append(result.Problems, fmt.Sprintf("read managed directory %s: %v", dir, err))
		return
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if wanted[path] {
			continue
		}
		claimable, claimErr := owns(path)
		if claimErr != nil {
			result.Warnings = append(
				result.Warnings,
				fmt.Sprintf("unreadable %s — cannot tell whether engine-sync owns it; left in place: %v", path, claimErr),
			)
			continue
		}
		if !claimable {
			continue
		}
		if mode != Build {
			result.Problems = append(result.Problems, string(Orphan)+" "+path)
			result.Rebuildable = append(result.Rebuildable, string(Orphan)+" "+path)
			result.Actions = append(result.Actions, Action{Kind: actionDelete, Path: path})
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			result.Problems = append(result.Problems, fmt.Sprintf("remove orphan %s: %v", path, err))
			continue
		}
		result.Actions = append(result.Actions, Action{Kind: actionDelete, Path: path})
		result.Deleted++
	}
}

// Claimable checks a header or a link into an explicit source root.
func Claimable(path string, roots []artifact.Root) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return false, err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		target = filepath.Clean(target)
		for _, root := range roots {
			for _, source := range root.SourceRoots {
				relative, err := filepath.Rel(source, target)
				if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					return true, nil
				}
			}
		}
		if len(roots) == 0 {
			return ClaudeTarget(target), nil
		}
		return false, nil
	case info.Mode().IsRegular():
		raw, err := os.ReadFile(path)
		return artifact.Claims(string(raw)), err
	case info.IsDir():
		raw, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
		if os.IsNotExist(err) {
			return false, nil
		}
		return artifact.Claims(string(raw)), err
	}
	return false, nil
}
func ClaudeTarget(target string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(target)), "/")
	for i, part := range parts[:len(parts)-1] {
		if part == ".claude" {
			switch parts[i+1] {
			case "agents", "commands", "skills":
				return true
			}
		}
	}
	return false
}
