package codex

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/rewrite"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"strings"
)

// TransformOptions contains only deterministic text substitutions. The
// compiler supplies the command roster discovered from source files.
type TransformOptions struct {
	RetiredFiles      []string
	Rewrites          []config.Rewrite
	Kind, Path        string
	Source            *claudesource.Project
	ModelMap          map[string]string
	Commands          map[string]string
	ReplaceClaudeFile bool
}

func transformMarkdown(text string, options TransformOptions) string {
	ctx := rewrite.Context{Engine: "codex", Kind: options.Kind, Source: options.Path, Roster: options.Commands, ModelMap: options.ModelMap, Naming: codexCommandNaming}
	rules := rewrite.Builtins(rewrite.Func(func(text string, ctx rewrite.Context) string { return rewriteCodeReview(text, ctx.ModelMap) }))
	rules = append(rules, rewrite.Configured(options.Rewrites)...)
	text = rules.Apply(text, ctx)
	if options.ReplaceClaudeFile {
		text = codexOrientationReferences(text)
	}
	return text
}

func codexOrientationReferences(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "AGENTS.md") {
			lines[i] = strings.ReplaceAll(line, "CLAUDE.md", "AGENTS.md")
		}
	}
	return strings.Join(lines, "\n")
}

func codexCommandNaming(name string) string {
	if strings.HasPrefix(name, "$") {
		return name
	}
	return "$" + name
}

func swapCommands(text string, roster map[string]string) string {
	return rewrite.CommandsIn(text, roster, codexCommandNaming)
}

func isCommandPrefixChar(value byte) bool { return rewrite.PrefixChar(value) }

func isCommandSuffixChar(value byte) bool { return rewrite.SuffixChar(value) }
