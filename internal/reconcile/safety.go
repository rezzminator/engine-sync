package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/atomicfile"
	"github.com/rezzminator/engine-sync/internal/paths"
	"os"
	"path/filepath"
	"strings"
)

func inside(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func scratchName(name string) bool {
	return strings.HasPrefix(name, ".") && (strings.Contains(name, ".tmp-") || strings.Contains(name, ".engine-sync-link-"))
}
func safeParents(path string, roots []artifact.Root) error {
	for _, root := range roots {
		if !inside(root.Path, path) {
			continue
		}
		boundary := root.Boundary
		if boundary == "" {
			boundary = filepath.Dir(root.Path)
		}
		for p := filepath.Dir(path); inside(boundary, p); p = filepath.Dir(p) {
			info, e := os.Lstat(p)
			if e != nil && !os.IsNotExist(e) {
				return fmt.Errorf("inspect output parent %s: %w", p, e)
			}
			if e == nil && info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("CONFLICT output parent %s is a symlink; preserved", p)
			}
			if p == boundary || p == filepath.Dir(p) {
				break
			}
		}
	}
	return nil
}
func receiptPath(path string) (string, error) {
	home, e := paths.Home()
	if e != nil {
		return "", e
	}
	p, e := paths.PhysicalPath(filepath.Dir(path))
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256([]byte(filepath.Join(p, filepath.Base(path))))
	return filepath.Join(paths.EngineSyncState(paths.Getenv, home), "links", hex.EncodeToString(sum[:])+".json"), nil
}
func recordLink(path, target string) error {
	p, e := receiptPath(path)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(struct{ Path, Target string }{path, target})
	if e != nil {
		return e
	}
	return atomicfile.Write(p, raw, 0600)
}
func ownedLink(path, target string) (bool, error) {
	p, e := receiptPath(path)
	if e != nil {
		return false, e
	}
	raw, e := paths.ReadRegular(p)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var record struct{ Path, Target string }
	if e = json.Unmarshal(raw, &record); e != nil {
		return false, fmt.Errorf("parse link receipt %s: %w", p, e)
	}
	return record.Target == target, nil
}

// Claimable requires a complete marker or a receipt for this exact link.
func Claimable(path string, roots []artifact.Root) (bool, error) {
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, e := os.Readlink(path)
		if e != nil {
			return false, e
		}
		return ownedLink(path, target)
	}
	if info.IsDir() {
		path = filepath.Join(path, "SKILL.md")
		info, e = os.Lstat(path)
		if os.IsNotExist(e) {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("inspect %s: not a regular marker file", path)
		}
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	raw, e := paths.ReadHeader(path)
	return artifact.Claims(string(raw)), e
}
func removeOwned(result *Result, path string, mode Mode, owns func(string) (bool, error)) {
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return
	}
	if e != nil {
		result.Problems = append(result.Problems, "inspect "+path+": "+e.Error())
		return
	}
	if info.IsDir() {
		entries, e := os.ReadDir(path)
		if e != nil {
			result.Problems = append(result.Problems, "read "+path+": "+e.Error())
			return
		}
		foreign := []string{}
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			info, err := os.Lstat(child)
			if err != nil {
				result.Problems = append(result.Problems, "inspect "+child+": "+err.Error())
				continue
			}
			owned, err := owns(child)
			if err != nil {
				result.Problems = append(result.Problems, "inspect ownership "+child+": "+err.Error())
			} else if !owned && !info.IsDir() {
				foreign = append(foreign, entry.Name())
			}
			if !scratchName(entry.Name()) {
				removeOwned(result, filepath.Join(path, entry.Name()), mode, owns)
			}
		}
		if mode != Build && len(foreign) > 0 {
			result.Problems = append(result.Problems, "CONFLICT "+path+": foreign entries preserved: "+strings.Join(foreign, ", "))
		}
		if mode == Build {
			entries, e = os.ReadDir(path)
			if e != nil {
				result.Problems = append(result.Problems, "read "+path+": "+e.Error())
				return
			}
			if len(entries) == 0 {
				if e := os.Remove(path); e != nil {
					result.Problems = append(result.Problems, "remove "+path+": "+e.Error())
				}
			} else {
				names := []string{}
				for _, entry := range entries {
					names = append(names, entry.Name())
				}
				result.Problems = append(result.Problems, "CONFLICT "+path+": foreign entries preserved: "+strings.Join(names, ", "))
			}
		}
		return
	}
	owned, e := owns(path)
	if e != nil {
		result.Problems = append(result.Problems, "inspect ownership "+path+": "+e.Error())
		return
	}
	if !owned {
		return
	}
	if mode != Build {
		message := string(Orphan) + " " + path
		result.Problems = append(result.Problems, message)
		result.Rebuildable = append(result.Rebuildable, message)
		result.Actions = append(result.Actions, Action{Kind: actionDelete, Path: path})
		return
	}
	if e := os.Remove(path); e != nil {
		result.Problems = append(result.Problems, "remove "+path+": "+e.Error())
		return
	}
	result.Deleted++
	result.Actions = append(result.Actions, Action{Kind: actionDelete, Path: path})
}

// RetireOwned clears only marker-owned files and recorded links, preserving foreign entries.
func RetireOwned(path string, mode Mode, boundary ...string) Result {
	result := Result{}
	root := artifact.Root{Path: path}
	if len(boundary) > 0 {
		root.Boundary = boundary[0]
	}
	if e := safeParents(path, []artifact.Root{root}); e != nil {
		result.Problems = append(result.Problems, e.Error())
		return result
	}
	removeOwned(&result, path, mode, func(p string) (bool, error) { return Claimable(p, nil) })
	return result
}
