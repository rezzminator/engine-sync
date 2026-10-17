package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrHomeRoot means the selected root is too broad to be a project.
var ErrHomeRoot = errors.New("is HOME or an ancestor of HOME; select a repository")

// ErrNoProjectRoot means discovery reached the filesystem root without a marker.
var ErrNoProjectRoot = errors.New("no project root found (expected configured marker, .git or CLAUDE.md)")

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
		return "", fmt.Errorf("project root %s %w", physical, ErrHomeRoot)
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
			return "", ErrNoProjectRoot
		}
		dir = parent
	}
}
