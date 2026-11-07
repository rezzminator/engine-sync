package rewrite

import "strings"

type Context struct {
	Engine, Kind, Source string
	Roster, ModelMap     map[string]string
	Naming               func(string) string
	// Agents maps each compiled Claude plugin agent id ("{plugin}:{name}")
	// to its twin name; PluginRoot is the resolved root of the plugin the
	// source ships in, empty for any other source.
	Agents     map[string]string
	PluginRoot string
}
type Rule interface{ Apply(string, Context) string }
type Func func(string, Context) string

func (f Func) Apply(text string, ctx Context) string { return f(text, ctx) }

type Chain []Rule

func (chain Chain) Apply(text string, ctx Context) string {
	for _, rule := range chain {
		text = rule.Apply(text, ctx)
	}
	return text
}

type Models struct{}

func (Models) Apply(text string, ctx Context) string { return ModelsIn(text, ctx.ModelMap) }

type Commands struct{}

func (Commands) Apply(text string, ctx Context) string {
	return CommandsIn(text, ctx.Roster, ctx.Naming)
}

type Agents struct{}

func (Agents) Apply(text string, ctx Context) string { return AgentsIn(text, ctx.Agents) }

// PluginRootVariable is the variable Claude Code substitutes in a plugin's
// agent, command and skill bodies with the plugin's root.
const PluginRootVariable = "${CLAUDE_PLUGIN_ROOT}"

type PluginRoot struct{}

func (PluginRoot) Apply(text string, ctx Context) string { return PluginRootIn(text, ctx.PluginRoot) }

// PluginRootIn replaces every ${CLAUDE_PLUGIN_ROOT} with root; an empty root
// leaves text as it is.
func PluginRootIn(text, root string) string {
	if root == "" {
		return text
	}
	return strings.ReplaceAll(text, PluginRootVariable, root)
}

type Literal struct {
	Engines, Kinds []string
	Text, Replace  string
}

func (rule Literal) Apply(text string, ctx Context) string {
	if !includes(rule.Engines, ctx.Engine) || !includes(rule.Kinds, ctx.Kind) {
		return text
	}
	return strings.ReplaceAll(text, rule.Text, rule.Replace)
}
func includes(values []string, value string) bool {
	if len(values) == 0 {
		return true
	}
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func Builtins(targetRules ...Rule) Chain {
	return append(Chain{Models{}, Commands{}, Agents{}, PluginRoot{}}, targetRules...)
}
