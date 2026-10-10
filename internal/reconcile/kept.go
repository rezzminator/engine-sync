package reconcile

import (
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// KeepArtifact leaves output, the generated output of the unresolvable source link
// source → target, exactly as it is and says so when it exists. An output that
// was never generated has nothing to keep and returns neither; one that
// cannot be inspected is a problem, never a silent keep.
func KeepArtifact(output, source, target string) (warning, problem string) {
	if _, err := os.Lstat(output); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ""
		}
		return "", fmt.Sprintf("inspect kept output %s: %v", output, err)
	}
	return fmt.Sprintf("source unresolvable: %s → %s; output kept", source, target), ""
}

// ExpandKept resolves prefix reservations against disk before orphan classification.
func ExpandKept(outputs []artifact.Artifact) ([]artifact.Artifact, []string) {
	var expanded []artifact.Artifact
	var problems []string
	for _, a := range outputs {
		if a.Kept == nil || a.Kept.Prefix == "" {
			expanded = append(expanded, a)
			continue
		}
		entries, err := os.ReadDir(filepath.Dir(a.Path))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			problems = append(problems, "read "+filepath.Dir(a.Path)+": "+err.Error())
			continue
		}
		for _, entry := range entries {
			if entry.Name() == filepath.Base(a.Path) || strings.HasPrefix(entry.Name(), a.Kept.Prefix) {
				b := a
				b.Path = filepath.Join(filepath.Dir(a.Path), entry.Name())
				b.Kept = &artifact.SourceLink{Path: a.Kept.Path, Target: a.Kept.Target}
				expanded = append(expanded, b)
			}
		}
	}
	return expanded, problems
}
