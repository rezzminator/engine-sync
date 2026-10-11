package reconcile

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/paths"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func Prepare(outputs []artifact.Artifact, managed ...[]artifact.Root) ([]artifact.Artifact, []string, []string) {
	var warnings, problems []string
	roots := []artifact.Root{}
	if len(managed) > 0 {
		roots = managed[0]
	}
	seen := map[string]artifact.Artifact{}
	for i, a := range outputs {
		if a.Kept != nil {
			continue
		}
		if e := safeParents(a.Path, roots); e != nil {
			problems = append(problems, e.Error())
			continue
		}
		if a.Kind != artifact.Link {
			if info, e := os.Lstat(a.Path); e == nil && info.Mode()&os.ModeSymlink != 0 {
				target, e := filepath.EvalSymlinks(a.Path)
				if e != nil {
					problems = append(problems, "CONFLICT "+a.Path+": "+e.Error())
					continue
				}
				allowed := false
				if a.Kind == artifact.Region {
					for _, r := range roots {
						base := r.Path
						if base == a.Path {
							base = filepath.Dir(base)
						}
						allowed = allowed || inside(base, target)
					}
				}
				if !allowed {
					problems = append(problems, "CONFLICT "+a.Path+": symlink preserved outside managed root")
					continue
				}
				a.Path = target
				outputs[i] = a
			}
		}
		key := paths.FoldName(filepath.Clean(a.Path))
		if prior, ok := seen[key]; ok && a.Kept == nil && prior.Kept == nil {
			problems = append(problems, "CONFLICT duplicate output "+a.Path+" from "+prior.Source+" and "+a.Source)
		}
		seen[key] = a
	}
	expanded, p := ExpandKept(outputs)
	outputs = expanded
	problems = append(problems, p...)
	for i, a := range outputs {
		if a.Merge == nil {
			continue
		}
		raw, err := paths.ReadRegular(a.Path)
		if err != nil && !os.IsNotExist(err) {
			problems = append(problems, "read "+a.Path+": "+err.Error())
			continue
		}
		if a.Kind != artifact.Region && len(raw) > 0 && !artifact.Claims(string(raw)) {
			continue
		}
		content, w, p := a.Merge(raw)
		warnings = append(warnings, w...)
		problems = append(problems, p...)
		if len(p) > 0 {
			outputs[i].Kept = &artifact.SourceLink{Path: a.Source}
			continue
		}
		outputs[i].Content = content
	}
	return outputs, warnings, problems
}

// Inspect reads existing target surfaces for pure validators, without following directory links.
func Inspect(roots []artifact.Root, desired []artifact.Artifact) ([]artifact.Artifact, []string) {
	seen := map[string]bool{}
	known := map[string]bool{}
	for _, a := range desired {
		known[a.Path] = true
	}
	validateForeign := false
	suffix := ""
	var outputs []artifact.Artifact
	var problems []string
	visit := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry == nil {
				return err
			}
			if !os.IsNotExist(err) {
				problems = append(problems, "inspect "+path+": "+err.Error())
			}
			return nil
		}
		if entry.IsDir() || seen[path] || (suffix != "" && !strings.HasSuffix(path, suffix) && !known[path]) {
			return nil
		}
		seen[path] = true
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			info, err = os.Stat(path)
		}
		if err != nil {
			if !os.IsNotExist(err) {
				problems = append(problems, "read "+path+": "+err.Error())
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		raw, err := paths.ReadRegular(path)
		if err != nil {
			if !os.IsNotExist(err) {
				problems = append(problems, "read "+path+": "+err.Error())
			}
			return nil
		}
		if !validateForeign && !known[path] && !artifact.Claims(string(raw)) {
			return nil
		}
		existing := artifact.Artifact{Path: path, Content: raw}
		for _, wanted := range desired {
			if wanted.Path == path {
				existing.NativeMode = wanted.NativeMode
				break
			}
		}
		outputs = append(outputs, existing)
		return nil
	}
	for _, root := range roots {
		validateForeign = root.ValidateOnly || root.InspectForeign
		suffix = root.Suffix
		inspectPath := root.Path
		info, statErr := os.Lstat(root.Path)
		if statErr == nil && info.IsDir() {
			inspectPath = filepath.Join(root.Path, ".scan")
		}
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			resolved, e := os.Stat(root.Path)
			if e == nil && resolved.IsDir() {
				problems = append(problems, "CONFLICT output directory "+root.Path+" is a symlink; preserved")
				continue
			}
		}
		if e := safeParents(inspectPath, []artifact.Root{root}); e != nil {
			problems = append(problems, e.Error())
			continue
		}

		if err := filepath.WalkDir(root.Path, visit); err != nil && !os.IsNotExist(err) {
			problems = append(problems, "inspect "+root.Path+": "+err.Error())
		}
	}
	for _, a := range desired {
		if seen[a.Path] {
			continue
		}
		info, err := os.Lstat(a.Path)
		if err != nil {
			if !os.IsNotExist(err) {
				problems = append(problems, "inspect "+a.Path+": "+err.Error())
			}
			continue
		}
		_ = visit(a.Path, fs.FileInfoToDirEntry(info), nil)
	}
	return outputs, problems
}
