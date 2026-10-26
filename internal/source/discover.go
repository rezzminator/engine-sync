package source

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Entry struct {
	Path, Rel, Target string
	SkillDir, DirLink bool
}
type Reporter struct {
	Problem, Warning func(string)
	Dangling         func(string, string, error)
}

func Markdown(dir string, excludes []string, report Reporter) []Entry {
	entries := []Entry{}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return entries
	} else if err != nil {
		report.Problem(fmt.Sprintf("inspect %s: %v", dir, err))
		return entries
	}
	visited := map[string]bool{}
	var walk func(string, string)
	walk = func(current, relative string) {
		resolved, evalErr := filepath.EvalSymlinks(current)
		if evalErr != nil {
			if os.IsNotExist(evalErr) {
				report.Dangling(current, relative, evalErr)
			} else {
				report.Problem(fmt.Sprintf("resolve %s: %v", current, evalErr))
			}
			return
		}
		if visited[resolved] {
			report.Warning("skip cyclic command directory " + current)
			return
		}
		visited[resolved] = true
		defer delete(visited, resolved)
		read, err := os.ReadDir(current)
		if err != nil {
			report.Problem(fmt.Sprintf("read %s: %v", current, err))
			return
		}
		for _, item := range read {
			name := item.Name()
			rel := name
			if relative != "" {
				rel = filepath.Join(relative, name)
			}
			if Excluded(excludes, rel, name) {
				continue
			}
			path := filepath.Join(current, name)
			info, statErr := os.Stat(path)
			if item.Type()&os.ModeSymlink != 0 {
				if statErr != nil {
					if os.IsNotExist(statErr) {
						report.Dangling(path, rel, statErr)
					} else {
						report.Problem(fmt.Sprintf("stat %s: %v", path, statErr))
					}
					continue
				}
			}
			if statErr != nil {
				report.Problem(fmt.Sprintf("stat %s: %v", path, statErr))
				continue
			}
			if info.IsDir() {
				walk(path, rel)
				continue
			}
			if name == "SKILL.md" {
				entries = append(entries, Entry{Path: filepath.Dir(path), Rel: filepath.Dir(rel), SkillDir: true})
				continue
			}
			if strings.HasSuffix(name, ".md") && name != "README.md" {
				if !info.Mode().IsRegular() {
					report.Problem(fmt.Sprintf("source %s: not a regular file", path))
					continue
				}
				entries = append(entries, Entry{Path: path, Rel: rel})
			}
		}
	}
	walk(dir, "")
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

func Excluded(exclusions []string, relative, name string) bool {
	relative = filepath.ToSlash(relative)
	for _, value := range exclusions {
		value = filepath.ToSlash(value)
		if value == name || value == relative || strings.HasPrefix(relative, value+"/") || strings.Contains("/"+relative, "/"+value+"/") {
			return true
		}
	}
	return false
}

func Projects(root string, listed, excludes []string, report Reporter, markers ...string) []string {
	projects := []string{"."}
	if listed != nil {
		for _, project := range listed {
			if Excluded(excludes, project, project) {
				continue
			}
			if project != "." {
				if found, err := paths.HasWorkbenchManifest(filepath.Join(root, project), markers...); err != nil {
					report.Problem(err.Error())
					continue
				} else if found {
					report.Warning(project + " is a workbench: it builds as its own root, not as a child project")
					continue
				}
			}
			if project != "." {
				// A missing directory falls through to the CLAUDE.md check, which names it.
				if inside, err := insideRoot(root, project); err != nil && !os.IsNotExist(err) {
					report.Problem(fmt.Sprintf("listed project %s: %v", filepath.Join(root, project), err))
					continue
				} else if err == nil && !inside {
					report.Problem(fmt.Sprintf("listed project %s: does not resolve below the root %s", filepath.Join(root, project), root))
					continue
				}
				info, e := os.Stat(filepath.Join(root, project, "CLAUDE.md"))
				if e != nil || !info.Mode().IsRegular() {
					report.Problem(fmt.Sprintf("listed project %s: no regular CLAUDE.md (%v)", filepath.Join(root, project), e))
					continue
				}
				projects = append(projects, project)
			}
		}
	} else if entries, err := os.ReadDir(root); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || Excluded(excludes, entry.Name(), entry.Name()) || entry.Name() == ".claude" ||
				entry.Name() == ".codex" {
				continue
			}
			if found, err := paths.HasWorkbenchManifest(filepath.Join(root, entry.Name()), markers...); err != nil {
				report.Problem(err.Error())
				continue
			} else if found {
				continue
			}
			if _, e := os.Stat(filepath.Join(root, entry.Name(), "CLAUDE.md")); e != nil && !os.IsNotExist(e) {
				report.Problem(fmt.Sprintf("inspect project %s: %v", entry.Name(), e))
				continue
			}
			if HasOrientation(filepath.Join(root, entry.Name())) {
				projects = append(projects, entry.Name())
			}
		}
	} else {
		report.Problem(fmt.Sprintf("read repository root %s: %v", root, err))
	}
	sort.Strings(projects[1:])
	return projects
}

// insideRoot reports whether root/project, every symlink resolved, stays below root.
func insideRoot(root, project string) (bool, error) {
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, project))
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(base, resolved)
	if err != nil {
		return false, err
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

func HasOrientation(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "CLAUDE.md"))
	return err == nil && info.Mode().IsRegular()
}

func Agents(dir string, report Reporter) []Entry {
	items, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		report.Problem(fmt.Sprintf("read %s: %v", dir, err))
		return nil
	}
	var entries []Entry
	for _, item := range items {
		if !strings.HasSuffix(item.Name(), ".md") || item.Name() == "README.md" {
			continue
		}
		path := filepath.Join(dir, item.Name())
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) && item.Type()&os.ModeSymlink != 0 {
				report.Dangling(path, item.Name(), err)
				entries = append(entries, Entry{Path: path, Rel: item.Name(), Target: LinkTarget(path)})
			} else {
				report.Problem(fmt.Sprintf("stat %s: %v", path, err))
			}
			continue
		}
		if info.Mode().IsRegular() {
			entries = append(entries, Entry{Path: path, Rel: item.Name()})
		}
	}
	return entries
}
