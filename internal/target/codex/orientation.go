package codex

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
)

func generatedHeader(source string) string {
	return artifact.Marker(artifact.Markdown, source)
}

func compileSourceMarkdown(source string, options TransformOptions, problem func(string)) (string, bool) {
	raw, err := options.Source.Read(source)
	if err != nil {
		problem(fmt.Sprintf("read %s: %v", source, err))
		return "", false
	}
	options.Kind = "orientation"
	options.Path = source
	return transformMarkdown(string(raw), options), true
}

// RuleTransform returns the transform a global rule body gets: the one
// CLAUDE.md gets for AGENTS.md (command and model rewrites, the configured
// rewrites, CLAUDE.md references), with commandDir's commands as the roster.
// problems name a roster that could not be read, so a failure to look never
// ships a rule's /command untransformed as if no command existed; warnings
// name the command links the roster skipped.
func RuleTransform(commandDir string, policy config.Config) (transform func(body, source string) string, problems, warnings []string) {
	result := Result{}
	options := TransformOptions{Review: policy.Codex.Review, ModelMap: policy.Codex.ModelMap, Commands: discoverCommandRosterIn(commandDir, nil, &result), Rewrites: policy.Rewrites}
	return ruleTransformWith(options), result.Problems, result.Warnings
}

// projectRender is what every artifact of one project's render shares: its
// projects, its /command roster and the transform they set.
type projectRender struct {
	root      string
	cfg       Config
	result    Result
	projects  []string
	roster    map[string]string
	transform TransformOptions
	// kept names, by problem, the output it costs: that output stays as it is.
	kept map[string]string
}

// keep reports messages as problems of the output at path, built from
// source: a problem there keeps that output as it is and costs only it.
func (r *projectRender) keep(path, source string, messages ...string) artifact.Artifact {
	for _, message := range messages {
		r.result.Problems = append(r.result.Problems, message)
		r.kept[message] = path
	}
	return artifact.Artifact{Path: path, Kept: &artifact.SourceLink{Path: source}}
}

func newProjectRender(p *claudesource.Project, cfgAll config.Config) *projectRender {
	r := &projectRender{root: p.Root, cfg: cfgAll.Codex, kept: map[string]string{}}
	if p.Workbench {
		r.cfg.GlobalCommands = false
	}
	r.result = Result{Warnings: []string{}, Problems: []string{}, Dangling: []string{}, Source: p}
	r.projects = discoverProjects(r.root, r.cfg, &r.result, cfgAll.WorkbenchMarker)
	r.roster = discoverCommandRoster(r.root, r.cfg, &r.result)
	for _, rel := range cfgAll.CommandDirs {
		for name, value := range discoverCommandRosterIn(filepath.Join(r.root, rel), r.cfg.ExcludeDirs, &r.result) {
			r.roster[name] = value
		}
	}
	if p.SourceCommandDir != "" {
		for name, value := range discoverCommandRosterIn(p.SourceCommandDir, nil, &r.result) {
			r.roster[name] = value
		}
	}
	r.transform = TransformOptions{Review: r.cfg.Review, ModelMap: r.cfg.ModelMap, Commands: r.roster, Source: r.result.Source, Rewrites: cfgAll.Rewrites}
	return r
}

// orientations compiles each project's AGENTS.md: its CLAUDE.md, then its own
// .claude/rules, then, at the root, codex.rootAdapter. One whose rules or
// adapter fail is kept as it is, its problems naming it.
func (r *projectRender) orientations() []artifact.Artifact {
	problem := func(message string) { r.result.Problems = append(r.result.Problems, message) }
	agentsDocumentTransform := r.transform
	agentsDocumentTransform.ReplaceClaudeFile = true
	var outputs []artifact.Artifact
	for _, project := range r.projects {
		src, path := filepath.Join(r.root, project, "CLAUDE.md"), filepath.Join(r.root, project, "AGENTS.md")
		content, ok := compileSourceMarkdown(src, agentsDocumentTransform, problem)
		if !ok {
			continue
		}
		if failed, ok := r.result.Source.RulesFailed[project]; ok {
			outputs = append(outputs, r.keep(path, src, failed))
			continue
		}
		rules := CompileRules(r.result.Source.Rules[project], filepath.ToSlash(filepath.Join(project, ".claude", "rules")), ruleTransformWith(r.transform))
		if len(rules.Broken) != 0 {
			outputs = append(outputs, r.keep(path, src, rules.Broken...))
			continue
		}
		if rules.Body != "" {
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			content += "\n" + rules.Body
		}
		if project == "." {
			adapter, err := config.RenderTemplate(r.cfg.RootAdapter, config.TemplateData{Engine: "Codex", Source: "CLAUDE.md", Root: "AGENTS.md", Models: r.cfg.ModelMap})
			if err != nil {
				outputs = append(outputs, r.keep(path, src, fmt.Sprintf("render codex.rootAdapter for %s: %v", src, err)))
				continue
			}
			content += adapter
		}
		rel, _ := filepath.Rel(r.root, src)
		outputs = append(outputs, artifact.Artifact{
			Path:    path,
			Content: []byte(generatedHeader(filepath.ToSlash(rel)) + "\n" + content), Source: src, Syntax: artifact.Markdown,
		})
	}
	return outputs
}

// Orientations is every AGENTS.md the Codex build writes for p, which
// OpenCode reads too; problems name each one that could not compile, the
// source's and the /command roster's own problems left to the caller's report.
func Orientations(p claudesource.Project, cfgAll config.Config) (orientations []artifact.Artifact, problems []string) {
	r := newProjectRender(&p, cfgAll)
	before := len(r.result.Problems)
	orientations = r.orientations()
	return orientations, r.result.Problems[before:]
}
