package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func ProjectRoot(root, home string) (string, error) {
	physical, e := PhysicalPath(root)
	if e != nil {
		return "", fmt.Errorf("project root %s: %w", root, e)
	}
	h, e := PhysicalPath(home)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(physical, h)
	if e == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
		return "", fmt.Errorf("project root %s is HOME or an ancestor of HOME; select a repository", physical)
	}
	return physical, nil
}
func DiscoverRoot(start, home, marker string) (string, error) {
	dir, e := PhysicalPath(start)
	if e != nil {
		return "", e
	}
	for {
		found, e := HasWorkbenchManifest(dir, marker)
		if e != nil {
			return "", e
		}
		if found {
			return ProjectRoot(dir, home)
		}
		for _, name := range []string{".git", "CLAUDE.md"} {
			info, e := os.Lstat(filepath.Join(dir, name))
			if e == nil && (name == ".git" || info.Mode().IsRegular()) {
				return ProjectRoot(dir, home)
			}
			if e != nil && !errors.Is(e, fs.ErrNotExist) {
				return "", fmt.Errorf("inspect project root %s: %w", dir, e)
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no project root found (expected configured marker, .git or CLAUDE.md)")
		}
		dir = parent
	}
}
