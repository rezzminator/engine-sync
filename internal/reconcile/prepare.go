package reconcile

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"io/fs"
	"os"
	"path/filepath"
)

func Prepare(outputs []artifact.Artifact) ([]artifact.Artifact, []string, []string) {
	var warnings, problems []string
	outputs, problems = ExpandKept(outputs)
	for i, a := range outputs {
		if a.Merge == nil {
			continue
		}
		raw, err := os.ReadFile(a.Path)
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
		outputs[i].Content = content
	}
	return outputs, warnings, problems
}

// Inspect reads existing target surfaces for pure validators, without following directory links.
func Inspect(roots []artifact.Root, desired []artifact.Artifact) ([]artifact.Artifact, []string) {
	seen := map[string]bool{}
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
		if entry.IsDir() || seen[path] {
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
		raw, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				problems = append(problems, "read "+path+": "+err.Error())
			}
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

func ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func Remove(path string) error    { return os.Remove(path) }
func RemoveAll(path string) error { return os.RemoveAll(path) }
