package opencode

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
)

func compileOpenCodeSkills(
	root string,
	add func(artifact.Artifact),
	problem, skip func(string, ...any),
	dangling func(string, error),
	model *claudesource.Project,
) {
	dir := filepath.Join(root, ".claude", "skills")
	for _, entry := range model.SkillsIn(dir, openCodeReporter(problem, dangling)) {
		if entry.Target != "" {
			continue
		}
		dst := filepath.Join(root, ".opencode", "skills", entry.Rel)
		rel, err := filepath.Rel(filepath.Dir(dst), entry.Path)
		if err != nil {
			skip("link %s to %s: %v", dst, entry.Path, err)
			continue
		}
		add(artifact.Artifact{Path: dst, Target: rel})
	}

}
