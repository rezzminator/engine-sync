package codex

import (
	"fmt"
	"strings"

	"github.com/rezzminator/engine-sync/internal/artifact"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
)

// RuleSet is one rules directory compiled for Codex and OpenCode, which have
// no path trigger and no per-loop rule filter.
type RuleSet struct {
	// Body is every always-on rule, transformed, and every path-scoped rule's
	// read-on-match pointer, each under its `<!-- rule: … -->` line, in sorted
	// relative-path order; "" when none.
	Body string
	// Scoped are the path-scoped rules' paths.
	Scoped []string
	// Targeted are always-on rules with a rule-control `visible-to:` key:
	// compiled like any other, so they reach every Codex and OpenCode loop.
	Targeted []string
	Broken   []string // RULE-UNREADABLE, RULE-FRONTMATTER and RULE-FENCE rows: each rule skipped
}

// CompileRules compiles rules, read from the directory label names in the
// compiled text, with transform, the body transform CLAUDE.md gets for
// AGENTS.md.
func CompileRules(rules []claudesource.Rule, label string, transform func(body, source string) string) RuleSet {
	var set RuleSet
	var blocks []string
	for _, rule := range rules {
		if rule.ReadErr != nil {
			set.Broken = append(set.Broken, fmt.Sprintf("RULE-UNREADABLE %s: %v", rule.Path, rule.ReadErr))
			continue
		}
		if rule.ParseErr != nil {
			set.Broken = append(set.Broken, fmt.Sprintf("RULE-FRONTMATTER %s: %v", rule.Path, rule.ParseErr))
			continue
		}
		name := label + "/" + rule.Rel
		if rule.Scope != nil {
			set.Scoped = append(set.Scoped, rule.Path)
			blocks = append(blocks, "<!-- rule: "+name+" (path-scoped) -->\nBefore working on a file matching `"+
				strings.Join(rule.Scope, "`, `")+"`, read "+name+" and follow it.\n")
			continue
		}
		compiled := strings.TrimSpace(transform(rule.Body, rule.Path))
		if strings.Contains(compiled, artifact.RulesBegin) || strings.Contains(compiled, artifact.RulesEnd) {
			// Compiled, it would leave two fences in a Codex home's AGENTS.md,
			// which every later run then refuses.
			set.Broken = append(set.Broken, fmt.Sprintf("RULE-FENCE %s: the body holds the engine-sync rules fence line", rule.Path))
			continue
		}
		if rule.VisibleTo {
			set.Targeted = append(set.Targeted, rule.Path)
		}
		blocks = append(blocks, "<!-- rule: "+name+" -->\n"+compiled+"\n")
	}
	set.Body = strings.Join(blocks, "\n")
	return set
}

// ruleTransformWith is the transform a rule body gets under options: the one
// CLAUDE.md gets for AGENTS.md.
func ruleTransformWith(options TransformOptions) func(body, source string) string {
	options.Kind = "orientation"
	options.ReplaceClaudeFile = true
	return func(body, source string) string {
		options.Path = source
		return transformMarkdown(body, options)
	}
}
