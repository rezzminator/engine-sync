package codex

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/rewrite"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"strings"
)

func compileAgents(
	root string,
	projects []string,
	all config.Config,
	options TransformOptions,
	add func(artifact.Artifact),
	problem, warn func(string),
	result *Result,
) {
	options.Kind = "agent"
	cfg := all.Codex
	seen := map[string]string{}
	for _, project := range projects {
		dir := filepath.Join(root, project, ".claude", "agents")
		for _, entry := range markdownSources(dir, nil, result) {
			if entry.SkillDir || entry.DirLink {
				continue
			}
			name, register, err := claudesource.RoleName(claudesource.DeclaredName(options.Source, entry.Path), project, cfg)
			if err != nil {
				problem(fmt.Sprintf("%s: %v", entry.Path, err))
				continue
			}
			if !register {
				warn("skip agent " + entry.Path + " — never-register policy")
				continue
			}
			key := paths.FoldName(name)
			if previous, ok := seen[key]; ok {
				problem(fmt.Sprintf("%s role name %s from %s and %s", reconcile.Conflict, name, previous, entry.Path))
				continue
			}
			seen[key] = entry.Path

			if entry.Target != "" {
				add(keptArtifact(filepath.Join(root, ".codex", "agents", name+".toml"), entry))
				continue
			}
			_, err = options.Source.Read(entry.Path)
			if err != nil {
				problem(fmt.Sprintf("read %s: %v", entry.Path, err))
				continue
			}
			rel, _ := filepath.Rel(root, entry.Path)
			front, body, parseErr := options.Source.Frontmatter(entry.Path)
			if parseErr != nil {
				problem(claudesource.DescribeParseError(entry.Path, parseErr))
				continue
			}
			problems, warnings := claudesource.ValidateAgent(entry.Path, front, all.Agents.PassthroughKeys, false)
			for _, w := range warnings {
				warn(w)
			}
			if len(problems) > 0 {
				for _, p := range problems {
					problem(p)
				}
				continue
			}
			if missing := claudesource.Unmet(front.Requires, all.Capabilities); missing != "" {
				warn(claudesource.Skipped(name, missing))
				continue
			}
			if front.Engines.CodexOff() {
				continue
			}
			instructions, err := config.RenderTemplate(cfg.AgentPreamble, config.TemplateData{Name: name, Engine: "Codex", Source: filepath.ToSlash(rel), Root: "AGENTS.md", Models: cfg.ModelMap})
			if err != nil {
				problem(fmt.Sprintf("render codex.agentPreamble for %s: %v", entry.Path, err))
				continue
			}
			text, warning, err := renderRole(roleSpec{
				Header:       artifact.Marker(artifact.TOML, filepath.ToSlash(rel)) + "\n",
				Name:         name,
				Description:  transformMarkdown(front.Fields["description"], options),
				Instructions: instructions + transformMarkdown(strings.TrimSpace(body), options),
				ModelComment: true,
			}, front, cfg.ModelMap, entry.Path)
			if err != nil {
				problem(err.Error())
				continue
			}
			if warning != "" {
				warn(warning)
			}
			add(artifact.Artifact{Path: filepath.Join(root, ".codex", "agents", name+".toml"), Content: []byte(text), Source: entry.Path, Syntax: artifact.TOML})
		}
	}
}

// roleSpec is what differs between the project and the machine path of the
// one Codex role renderer: the marker header, the role's name, description
// and instructions as each path prepared them, and whether the project's
// `# model:` comment precedes the fields.
type roleSpec struct {
	Header, Name, Description, Instructions string
	ModelComment                            bool
}

// renderRole is the one Codex role renderer. warning names an unmapped model
// alias, whose model line is omitted.
func renderRole(spec roleSpec, front claudesource.Frontmatter, modelMap map[string]string, source string) (string, string, error) {
	role, warning, err := codexRoleSettings(front, modelMap, source)
	if err != nil {
		return "", "", err
	}
	text := spec.Header
	if spec.ModelComment && role.Model != "" {
		alias := strings.TrimSpace(front.Model)
		text += "# model: " + tomlEscape(role.Model)
		if alias != "" && alias != role.Model {
			text += " (Claude alias: " + tomlEscape(alias) + ")"
		}
		text += "\n"
	}
	text += "name = " + tomlString(spec.Name) + "\ndescription = " + tomlString(spec.Description) + "\n"
	if role.Model != "" {
		text += "model = " + tomlString(role.Model) + "\n"
	}
	if role.Effort != "" {
		text += "model_reasoning_effort = " + tomlString(role.Effort) + "\n"
	}
	if role.Sandbox != "" {
		text += "sandbox_mode = " + tomlString(role.Sandbox) + "\n"
	}
	text += "developer_instructions = \"\"\"\n" + tomlMultiline(spec.Instructions) + "\"\"\"\n"
	return text, warning, nil
}

// codexRoleSettings resolves a role's Codex model, effort and sandbox.
// Model: engines.codex.model (a codex.modelMap key is mapped, any other value
// is verbatim), then modelMap[model]; an unmapped alias is omitted with a
// warning, and Claude's "inherit" is no model line (the session's model).
// Effort: engines.codex.effort, then effort; max reaches Codex as xhigh.
// Sandbox: engines.codex.sandbox (inherit is no line), else read-only when
// the tools line grants no write tool.
func codexRoleSettings(front claudesource.Frontmatter, modelMap map[string]string, source string) (codexRole, string, error) {
	var role codexRole
	var warning string
	settings := claudesource.CodexEngine{}
	if front.Engines.Codex != nil {
		settings = *front.Engines.Codex
	}
	if pin := strings.TrimSpace(settings.Model); pin != "" {
		role.Model = pin
		if mapped, ok := modelMap[pin]; ok {
			role.Model = mapped
		}
	} else if model := strings.TrimSpace(front.Model); model != "" && model != claudeInheritModel {
		role.Model = modelMap[model]
		if strings.TrimSpace(role.Model) == "" {
			role.Model = ""
			warning = fmt.Sprintf("unmapped Codex model alias %q in %s — model omitted", model, source)
		}
	}
	role.Effort = strings.TrimSpace(front.Effort)
	if pin := strings.TrimSpace(settings.Effort); pin != "" {
		role.Effort = pin
	}
	if role.Effort != "" {
		mapped, ok := codeReviewEfforts[role.Effort]
		if !ok {
			return codexRole{}, "", fmt.Errorf("%s: invalid effort %q", source, role.Effort)
		}
		role.Effort = mapped
	}
	switch sandbox := strings.TrimSpace(settings.Sandbox); sandbox {
	case "inherit":
	case "":
		if codexReadOnly(front.Fields["tools"]) {
			role.Sandbox = "read-only"
		}
	default:
		role.Sandbox = sandbox
	}
	return role, warning, nil
}

// codexReadOnly reports whether a role compiles to sandbox_mode = "read-only",
// unless the Claude tools line grants a write tool,
// or the tools line is absent. Explicit codex-sandbox frontmatter takes precedence.
func codexReadOnly(tools string) bool {
	if strings.TrimSpace(tools) == "" {
		return false
	}
	for _, tool := range strings.Split(tools, ",") {
		switch strings.TrimSpace(tool) {
		case "Write", "Edit", "MultiEdit", "NotebookEdit":
			return false
		}
	}
	return true
}

// renderGlobalAgentTOML compiles one agent's Markdown into the role name and
// the exact TOML bytes its registry file holds. raw is passed in rather than
// read here because a variant's Markdown exists only in memory until the build
// phase writes it; mdPath labels errors and, against agentsDir, names the
// source the generated marker credits.
func renderGlobalAgentTOML(mdPath, raw, agentsDir string, policies ...config.Config) (string, string, string, error) {
	front, body, err := claudesource.Parse(raw)
	if err != nil {
		return "", "", "", fmt.Errorf("%s", claudesource.DescribeParseError(mdPath, err))
	}
	if front.Node == nil {
		return "", "", "", fmt.Errorf("%s: no frontmatter", mdPath)
	}
	name := strings.TrimSpace(front.Fields["name"])
	description := strings.TrimSpace(front.Fields["description"])
	if name == "" || description == "" {
		return "", "", "", fmt.Errorf("%s: frontmatter needs both name: and description", mdPath)
	}
	if err := paths.ValidateName(name); err != nil {
		return "", "", "", fmt.Errorf("%s: %w", mdPath, err)
	}
	body = strings.TrimSpace(body)

	cfg := config.Default()
	if len(policies) > 0 {
		cfg = policies[0]
	}
	generated := filepath.Dir(mdPath) != filepath.Clean(agentsDir)
	if problems, _ := claudesource.ValidateAgent(mdPath, front, cfg.Agents.PassthroughKeys, generated); len(problems) > 0 {
		return "", "", "", fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	ctx := rewrite.Context{Engine: "codex", Kind: "agent", Source: mdPath}
	rules := rewrite.Builtins(rewrite.Func(func(text string, _ rewrite.Context) string {
		return rewriteCodeReview(text, cfg.Codex.ModelMap, cfg.Codex.Review)
	}))
	description = rules.Apply(description, ctx)
	rules = append(rules, rewrite.Configured(cfg.Machine.Rewrites)...)
	rules = append(rules, rewrite.Configured(cfg.Rewrites)...)
	description = rewrite.Configured(append(cfg.Machine.Rewrites, cfg.Rewrites...)).Apply(description, ctx)
	body = rules.Apply(body, ctx)
	content, warning, err := renderRole(roleSpec{
		Header:       globalRoleHeader(globalAgentMarkerSource(mdPath, agentsDir, cfg.Machine.AgentMarkerDir)),
		Name:         name,
		Description:  description,
		Instructions: body,
	}, front, cfg.Codex.ModelMap, mdPath)
	if err != nil {
		return "", "", "", err
	}
	return name, content, warning, nil
}

// globalAgentMarkerSource is source.MachineMarkerSource: one credit rule for
// every engine's machine-global agent marker.
func globalAgentMarkerSource(mdPath, agentsDir string, markers ...string) string {
	markerDir := ""
	if len(markers) > 0 {
		markerDir = markers[0]
	}
	return claudesource.MachineMarkerSource(mdPath, agentsDir, markerDir)
}

// globalAgentEscape is for a TOML basic string — build-codex.mjs:151.
// Only a backslash and a raw quote need neutralising; name/description are
// always a single frontmatter line, so no literal newline ever reaches it.
func globalAgentEscape(s string) string { return tomlEscape(s) }

// globalAgentEscapeMultiline is for a TOML multi-line basic string —
// build-codex.mjs:153. Only a backslash and a literal triple-quote need
// neutralising; TOML tolerates a raw newline inside """ ... """.
func globalAgentEscapeMultiline(s string) string { return strings.TrimSuffix(tomlMultiline(s), "\n") }

const claudeInheritModel = "inherit"

type codexRole struct{ Model, Effort, Sandbox string }

func RenderMachineAgent(path, raw, dir string, cfg ...config.Config) (string, string, error) {
	name, content, _, err := renderGlobalAgentTOML(path, raw, dir, cfg...)
	return name, content, err
}
func ValidateRole(text string) error { return validateTOML(text) }

type Role = codexRole

func RoleSettings(f claudesource.Frontmatter, m map[string]string, s string) (Role, string, error) {
	return codexRoleSettings(f, m, s)
}
func EscapeRole(s string) string          { return globalAgentEscape(s) }
func EscapeRoleMultiline(s string) string { return globalAgentEscapeMultiline(s) }
