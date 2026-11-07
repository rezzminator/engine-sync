package opencode

import (
	"errors"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/rewrite"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"io/fs"
	"path/filepath"
	"strings"
)

func discoverOpenCodeCommandRoster(
	root string,
	roster map[string]string,
	problem func(string, ...any),
	dangling func(string, error),
	models ...*claudesource.Project,
) {
	for _, entry := range discoverOpenCodeMarkdown(root, problem, dangling, models...) {
		colon := claudesource.CommandName(entry.Rel)
		flat := openCodeFlatName(entry.Rel)
		if err := paths.ValidateName(flat); err != nil {
			problem("%s: %v", entry.Path, err)
			continue
		}
		if colon != flat {
			roster[colon] = "/" + flat
		}
	}
}

func openCodeReporter(problem func(string, ...any), dangling func(string, error)) claudesource.Reporter {
	return claudesource.Reporter{Problem: func(s string) { problem("%s", s) }, Warning: func(s string) { problem("command source symlink cycle: %s", s) }, Dangling: func(path, rel string, err error) { dangling(path, err) }}
}

func discoverOpenCodeMarkdown(root string, problem func(string, ...any), dangling func(string, error), models ...*claudesource.Project) []claudesource.Entry {
	var model *claudesource.Project
	if len(models) > 0 {
		model = models[0]
	}
	var out []claudesource.Entry
	for _, e := range model.Markdown(root, nil, openCodeReporter(problem, dangling)) {
		out = append(out, claudesource.Entry{Path: e.Path, Rel: e.Rel, SkillDir: e.SkillDir})
	}
	return out
}

func compileOpenCodeCommands(
	sourceRoot, sourceLabel string,
	roster map[string]string,
	outputRoot string,
	add func(artifact.Artifact),
	problem, skip func(string, ...any),
	dangling func(string, error),
	model *claudesource.Project,
	managedBy string,
	policies ...config.Config,
) {
	keepDangling := func(path string, err error) {
		dangling(path, err)
		name := filepath.Base(path)
		if !strings.HasSuffix(name, ".md") || name == "README.md" ||
			name == "SKILL.md" {
			return
		}
		target := model.LinkTarget(path)
		rel, relErr := filepath.Rel(sourceRoot, path)
		if relErr != nil {
			problem("relative path of %s: %v", path, relErr)
			return
		}
		add(artifact.Artifact{
			Path: filepath.Join(outputRoot, openCodeFlatName(rel)+".md"),
			Kept: &artifact.SourceLink{Path: path, Target: target},
		})
	}
	policy := config.Default()
	if len(policies) > 0 {
		policy = policies[0]
	}
	for _, entry := range discoverOpenCodeMarkdown(sourceRoot, problem, keepDangling, model) {
		compileOpenCodeCommand(entry, openCodeCommandSource{Label: sourceLabel, Roster: roster, Agents: model.AgentRoster}, outputRoot, add, problem, dangling, model, managedBy, policy)
	}
}

// openCodeCommandSource is what differs between a machine command and a
// plugin one: the marker label, the command roster, and for a plugin the
// agent roster and its root.
type openCodeCommandSource struct {
	Label          string
	Roster, Agents map[string]string
	// PluginRoot and Marker are a plugin source's root and marker credit.
	PluginRoot, Marker string
}

// compileOpenCodeCommand renders one command, or a skill's SKILL.md, into
// {outputRoot}/{flat name}.md.
func compileOpenCodeCommand(
	entry claudesource.Entry,
	source openCodeCommandSource,
	outputRoot string,
	add func(artifact.Artifact),
	problem func(string, ...any),
	dangling func(string, error),
	model *claudesource.Project,
	managedBy string,
	policy config.Config,
) {
	file := entry.Path
	if entry.SkillDir {
		file = filepath.Join(entry.Path, "SKILL.md")
	}
	_, err := model.Read(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			dangling(file, err)
		} else {
			problem("read %s: %v", file, err)
		}
		return
	}
	flat := openCodeFlatName(entry.Rel)
	if err := paths.ValidateName(flat); err != nil {
		problem("%s: %v", entry.Path, err)
		return
	}
	output := filepath.Join(outputRoot, flat+".md")
	front, body, parseErr := model.Frontmatter(file)
	fields := front.Fields
	if parseErr != nil {
		problem("%s", claudesource.DescribeParseError(file, parseErr))
		// A source that does not parse keeps its installed twin until it builds again.
		add(artifact.Artifact{Path: output, Kept: &artifact.SourceLink{Path: file}})
		return
	}
	chain := append(rewrite.Builtins(), rewrite.Configured(policy.Rewrites)...)
	ctx := rewrite.Context{Engine: "opencode", Kind: "command", Source: file, Roster: source.Roster, Agents: source.Agents, PluginRoot: source.PluginRoot}
	relSource := filepath.ToSlash(filepath.Join(source.Label, entry.Rel))
	if source.Marker != "" {
		relSource = source.Marker
	}
	content := artifact.WithManagedBy("---\n"+artifact.Marker(artifact.YAMLFront,
		relSource,
	)+"\ndescription: "+quoteOpenCodeYAML(
		chain.Apply(strings.TrimSpace(fields["description"]), ctx),
	)+"\n---\n"+chain.Apply(body, ctx), managedBy)
	add(artifact.Artifact{Path: output, Content: []byte(content), Source: entry.Path, Syntax: artifact.YAMLFront})
}
