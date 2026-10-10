package codex

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/rewrite"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"os"
	"path/filepath"
	"strings"
)

func discoverCommandRoster(root string, cfg Config, result *Result) map[string]string {
	return discoverCommandRosterIn(filepath.Join(root, ".claude", "commands"), cfg.ExcludeDirs, result)
}

func discoverCommandRosterIn(sourceRoot string, exclusions []string, result *Result) map[string]string {
	roster := map[string]string{}
	entries := discoverMarkdown(sourceRoot, exclusions, result)
	for _, entry := range entries {
		rel := entry.Rel
		if !entry.SkillDir {
			var err error
			rel, err = filepath.Rel(sourceRoot, entry.Path)
			if err != nil {
				continue
			}
		}
		name := colonName(filepath.ToSlash(rel))
		roster[name] = "$" + flatName(filepath.ToSlash(rel))
	}
	return roster
}

func discoverMarkdown(dir string, excludes []string, result *Result) []claudesource.Entry {
	entries := result.Source.Markdown(dir, excludes, codexReporter(result))
	out := make([]claudesource.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, claudesource.Entry{Path: e.Path, Rel: e.Rel, SkillDir: e.SkillDir})
	}
	return out
}

func compileRepoCommands(
	root string,
	cfg Config,
	options TransformOptions,
	_ map[string]string,
	add func(artifact.Artifact),
	problem, warn func(string),
	result *Result,
) {
	sourceRoot := filepath.Join(root, ".claude", "commands")
	for _, entry := range markdownSources(sourceRoot, cfg.ExcludeDirs, result) {
		if entry.Target != "" {
			if entry.DirLink || strings.HasSuffix(entry.Rel, ".md") {
				for _, twin := range keptDirArtifacts(root, entry, problem) {
					add(twin)
				}
				continue
			}
			add(keptCommandArtifact(root, entry))
			continue
		}
		if entry.SkillDir {
			dst := filepath.Join(root, ".codex", "skills", flatName(filepath.ToSlash(entry.Rel)))
			add(artifact.Artifact{Path: dst, Target: relativeLink(dst, entry.Path)})
			continue
		}
		compileCommandFile(
			root,
			sourceRoot,
			entry,
			".claude/commands",
			filepath.Join(root, ".codex", "skills"),
			options,
			add,
			problem,
			warn,
			result,
			false,
		)
	}
}

func compileGlobalCommands(
	_, sourceHome, outputHome string,
	_ Config,
	options TransformOptions,
	add func(artifact.Artifact),
	problem, warn func(string),
	result *Result,
) {
	sourceRoot := filepath.Join(sourceHome, ".claude", "commands")
	if options.Source != nil && options.Source.SourceCommandDir != "" {
		sourceRoot = options.Source.SourceCommandDir
	}
	for _, entry := range markdownSources(sourceRoot, nil, result) {
		if entry.Target != "" {
			for _, twin := range keptGlobalCommandArtifacts(outputHome, entry, problem) {
				add(twin)
			}
			continue
		}
		if entry.SkillDir {
			dst := filepath.Join(outputHome, ".codex", "skills", flatName(filepath.ToSlash(entry.Rel)))
			link := entry.Path
			add(artifact.Artifact{Path: dst, Target: link})
			continue
		}
		compileCommandFile(
			outputHome,
			sourceRoot,
			entry,
			"$HOME/.claude/commands",
			"",
			options,
			add,
			problem,
			warn,
			result,
			true,
		)
	}
}

// compileInstalledGlobalCommands is the single planner for installer-owned
// host commands. Full repository build/check and install reconciliation must
// use the same source roster and defaults or they can disagree about the
// bytes under $HOME/.codex immediately after a successful install.
func compileInstalledGlobalCommands(
	sourceHome, outputHome string,
	add func(artifact.Artifact),
	problem, warn func(string),
	result *Result,
	policies ...config.Config,
) {
	policy := config.Default()
	if len(policies) > 0 {
		policy = policies[0]
	}
	cfg := policy.Codex
	sourceRoot := filepath.Join(sourceHome, ".claude", "commands")
	if result.Source != nil && result.Source.SourceCommandDir != "" {
		sourceRoot = result.Source.SourceCommandDir
	}
	roster := discoverCommandRosterIn(sourceRoot, nil, result)
	transform := TransformOptions{Review: cfg.Review, ModelMap: cfg.ModelMap, Commands: roster, Source: result.Source, RetiredFiles: policy.Machine.RetiredFiles, Rewrites: policy.Rewrites}
	compileGlobalCommands(outputHome, sourceHome, outputHome, cfg, transform, add, problem, warn, result)
}

func compileCommandFile(
	outputBase, sourceRoot string,
	entry claudesource.Entry,
	label, outputRoot string,
	options TransformOptions,
	add func(artifact.Artifact),
	problem, _ func(string),
	result *Result,
	global bool,
) {
	options.Kind = "command"
	options.Path = entry.Path
	raw, err := options.Source.Read(entry.Path)
	if err != nil {
		if os.IsNotExist(err) {
			result.RecordDangling(entry.Path, err)
		} else {
			problem(fmt.Sprintf("read %s: %v", entry.Path, err))
		}
		return
	}
	rel, _ := filepath.Rel(sourceRoot, entry.Path)
	rel = filepath.ToSlash(rel)
	relSource := filepath.ToSlash(filepath.Join(label, rel))
	content := string(raw)
	front, body, parseErr := options.Source.Frontmatter(entry.Path)
	fields := front.Fields
	if parseErr != nil {
		problem(fmt.Sprintf("parse %s: %v", entry.Path, parseErr))
		return
	}
	flat := flatName(filepath.ToSlash(entry.Rel))
	if err := paths.ValidateName(flat); err != nil {
		problem(fmt.Sprintf("%s: %v", entry.Path, err))
		return
	}
	fm := claudesource.HeaderLines(content)
	_ = fields
	// The incumbent compilers only rewrite command references in command
	// frontmatter. Model names there are descriptive metadata, not executable
	// model selections; rewriting them changes user-facing skill descriptions.
	// A /code-review written there IS a command reference, and a Codex seat
	// reading the Claude spelling reviews the whole branch (review.go).
	header := rewriteCodeReviewFrontmatter(swapCommands(strings.Join(fm, "\n"), options.Commands), options.ModelMap, options.Review)
	header = rewrite.Configured(options.Rewrites).Apply(header, rewrite.Context{Engine: "codex", Kind: "command", Source: entry.Path})
	generated := "---\n" + artifact.Marker(artifact.YAMLFront,
		relSource,
	) + "\nname: " + flat + "\n" + header + "\n---\n" + transformMarkdown(
		body,
		options,
	)
	dst := filepath.Join(outputRoot, flat, "SKILL.md")
	if global {
		add(artifact.Artifact{Path: filepath.Join(outputBase, ".codex", "prompts", flat+".md"), Content: []byte(generated), Source: entry.Path, Syntax: artifact.YAMLFront})
		if fields["disable-model-invocation"] == "true" || retiredSkill(flat, options.RetiredFiles) {
			return
		}
		dst = filepath.Join(outputBase, ".codex", "skills", flat, "SKILL.md")
	}
	add(artifact.Artifact{Path: dst, Content: []byte(generated), Source: entry.Path, Syntax: artifact.YAMLFront})
}

func retiredSkill(name string, files []string) bool {
	for _, path := range files {
		if filepath.ToSlash(path) == ".codex/skills/"+name || filepath.ToSlash(path) == ".codex/skills/"+name+"/SKILL.md" {
			return true
		}
	}
	return false
}

func flatName(rel string) string {
	rel = strings.TrimSuffix(filepath.ToSlash(rel), ".md")
	return strings.ReplaceAll(rel, "/", "-")
}

func colonName(rel string) string {
	rel = strings.TrimSuffix(filepath.ToSlash(rel), ".md")
	return strings.ReplaceAll(rel, "/", ":")
}
