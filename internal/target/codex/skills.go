package codex

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
)

func compileRepoSkills(root string, add func(artifact.Artifact), problem func(string), result *Result) {
	dir := filepath.Join(root, ".claude", "skills")
	for _, entry := range result.Source.SkillsIn(dir, codexReporter(result)) {
		dst := filepath.Join(root, ".codex", "skills", entry.Rel)
		if entry.Target != "" {
			add(keptTwin(dst, claudesource.Entry{Path: entry.Path, Rel: entry.Rel, Target: entry.Target}))
			continue
		}
		add(artifact.Artifact{Path: dst, Target: relativeLink(dst, entry.Path)})
	}

}

func relativeLink(dst, source string) string {
	rel, err := filepath.Rel(filepath.Dir(dst), source)
	if err != nil {
		return source
	}
	return rel
}
