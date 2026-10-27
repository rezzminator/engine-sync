package codex

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
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
	options := TransformOptions{Review: policy.Codex.Review, ModelMap: policy.Codex.ModelMap, Commands: discoverCommandRosterIn(commandDir, nil, &result), Rewrites: policy.Rewrites, ReplaceClaudeFile: true, Kind: "orientation"}
	return func(body, source string) string {
		options.Path = source
		return transformMarkdown(body, options)
	}, result.Problems, result.Warnings
}
