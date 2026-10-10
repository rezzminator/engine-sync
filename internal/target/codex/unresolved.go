package codex

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"sort"
	"strings"
)

const skillFileName = "SKILL.md"

// markdownSources is discoverMarkdown plus every unresolvable .md or directory link
// that walk recorded, each marked by its target. A source that is truly gone
// (no file and no link) appears in neither list, so its twin stays an orphan.
func markdownSources(dir string, excludes []string, result *Result) []claudesource.Entry {
	before := len(result.LeafLinks)
	entries := discoverMarkdown(dir, excludes, result)
	for _, leaf := range result.LeafLinks[before:] {
		name := filepath.Base(leaf.Path)
		if name == skillFileName {
			skill := leaf
			skill.Rel, skill.DirLink, skill.SkillDir = filepath.Dir(leaf.Rel), true, true
			entries = append(entries, skill)
		}
		if strings.HasSuffix(name, ".md") {
			if name == "README.md" || name == skillFileName {
				leaf.DirLink = true
			}
			entries = append(entries, leaf)
		} else {
			leaf.DirLink = true
			entries = append(entries, leaf)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

// keptArtifact is the output standing for an unresolvable source's twin at path.
func keptArtifact(path string, source claudesource.Entry) artifact.Artifact {
	return artifact.Artifact{Path: path, Kept: &artifact.SourceLink{Path: source.Path, Target: source.Target}}
}

// keptCommandArtifact is keptArtifact at the repo command skill compileCommandFile
// writes for entry.
func keptCommandArtifact(root string, entry claudesource.Entry) artifact.Artifact {
	return keptArtifact(
		filepath.Join(root, ".codex", "skills", flatName(filepath.ToSlash(entry.Rel)), skillFileName),
		entry,
	)
}

func keptDirArtifacts(root string, entry claudesource.Entry, problem func(string)) []artifact.Artifact {
	prefix := ""
	if !entry.SkillDir {
		prefix = strings.ReplaceAll(filepath.ToSlash(entry.Rel), "/", "-") + "-"
	}
	return []artifact.Artifact{{Path: filepath.Join(root, ".codex", "skills", flatName(filepath.ToSlash(entry.Rel))), Kept: &artifact.SourceLink{Path: entry.Path, Target: entry.Target, Prefix: prefix}}}
}

// keptGlobalCommandArtifacts preserves both global surfaces of an unresolved command.
func keptGlobalCommandArtifacts(home string, entry claudesource.Entry, problem func(string)) []artifact.Artifact {
	outputs := keptDirArtifacts(home, entry, problem)
	if entry.SkillDir {
		return outputs
	}
	prefix := strings.ReplaceAll(filepath.ToSlash(entry.Rel), "/", "-") + "-"
	outputs = append(outputs, artifact.Artifact{Path: filepath.Join(home, ".codex", "prompts", flatName(filepath.ToSlash(entry.Rel))+".md"), Kept: &artifact.SourceLink{Path: entry.Path, Target: entry.Target, Prefix: prefix}})
	return outputs
}
