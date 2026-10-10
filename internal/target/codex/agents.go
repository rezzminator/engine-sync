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
	cfg Config,
	options TransformOptions,
	add func(artifact.Artifact),
	problem, warn func(string),
	result *Result,
) {
	options.Kind = "agent"
	seen := map[string]string{}
	for _, project := range projects {
		dir := filepath.Join(root, project, ".claude", "agents")
		for _, entry := range markdownSources(dir, nil, result) {
			if entry.SkillDir || entry.DirLink {
				continue
			}
			base := strings.TrimSuffix(filepath.Base(entry.Path), ".md")
			name, register, err := claudesource.RoleName(base, project, cfg)
			if err != nil {
				problem(fmt.Sprintf("%s: %v", entry.Path, err))
				continue
			}
			if !register {
				warn("skip agent " + entry.Path + " — never-register policy")
				continue
			}
			key := paths.FoldName(strings.ReplaceAll(name, "-", "_"))
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
			fields := front.Fields
			if parseErr != nil {
				problem(fmt.Sprintf("parse %s: %v", entry.Path, parseErr))
				continue
			}
			role, err := codexRoleSettings(fields, cfg.ModelMap, entry.Path)
			if err != nil {
				problem(err.Error())
				continue
			}
			modelAlias := fields["model"]
			if modelAlias != "" && modelAlias != "inherit" && cfg.ModelMap[modelAlias] == "" && fields["codex-model"] == "" {
				warn(fmt.Sprintf("unmapped Codex model alias %q in %s — model omitted", modelAlias, entry.Path))
			}
			tomlName := strings.ReplaceAll(name, "-", "_")
			instructions, err := config.RenderTemplate(cfg.AgentPreamble, config.TemplateData{Name: tomlName, Engine: "Codex", Source: filepath.ToSlash(rel), Root: "AGENTS.md"})
			if err != nil {
				problem(fmt.Sprintf("render codex.agentPreamble for %s: %v", entry.Path, err))
				continue
			}
			instructions += transformMarkdown(strings.TrimSpace(body), options)
			toml := artifact.Marker(artifact.TOML, filepath.ToSlash(rel)) + "\n"
			if role.Model != "" {
				toml += "# model: " + tomlEscape(role.Model)
				if modelAlias != "" && modelAlias != role.Model {
					toml += " (Claude alias: " + tomlEscape(modelAlias) + ")"
				}
				toml += "\n"
			}
			description := transformMarkdown(fields["description"], options)
			toml += "name = " + tomlString(tomlName) + "\ndescription = " + tomlString(description) + "\n"
			if role.Model != "" {
				toml += "model = " + tomlString(role.Model) + "\n"
			}
			if role.Effort != "" {
				toml += "model_reasoning_effort = " + tomlString(role.Effort) + "\n"
			}
			if role.Sandbox != "" {
				toml += "sandbox_mode = " + tomlString(role.Sandbox) + "\n"
			}
			toml += "developer_instructions = \"\"\"\n" + tomlMultiline(instructions) + "\"\"\"\n"
			add(artifact.Artifact{Path: filepath.Join(root, ".codex", "agents", name+".toml"), Content: []byte(toml), Source: entry.Path, Syntax: artifact.TOML})
		}
	}
}

func codexRoleSettings(fields, modelMap map[string]string, source string) (codexRole, error) {
	role := codexRole{Effort: strings.TrimSpace(fields["effort"])}
	if override, ok := fields["codex-model"]; ok {
		role.Model = strings.TrimSpace(override)
	} else if model := strings.TrimSpace(fields["model"]); model != "" {
		mapped, ok := modelMap[model]
		if !ok && model == claudeInheritModel {
			// Claude's "inherit" is the session's model: a Codex role with no
			// model line runs on its session's model too.
			mapped, ok = "", true
		}
		if !ok {
			mapped = ""
		}

		role.Model = mapped
	}
	if role.Effort != "" {
		if mapped, ok := codeReviewEfforts[role.Effort]; ok {
			role.Effort = mapped
		} else {
			return codexRole{}, fmt.Errorf("%s: invalid effort %q", source, role.Effort)
		}
	}
	if override, ok := fields["codex-effort"]; ok {
		role.Effort = strings.TrimSpace(override)
		switch role.Effort {
		case codeReviewBareEffort, codexMediumEffort, codexHighEffort, codeReviewTopEffort:
		default:
			return codexRole{}, fmt.Errorf(
				"%s: invalid codex-effort %q (want low, medium, high, or xhigh)",
				source,
				role.Effort,
			)
		}
	}
	if sandbox, ok := fields["codex-sandbox"]; ok {
		if strings.TrimSpace(sandbox) != "workspace-write" && strings.TrimSpace(sandbox) != "inherit" && strings.TrimSpace(sandbox) != "read-only" {
			return codexRole{}, fmt.Errorf("%s: invalid codex-sandbox %q (want inherit, read-only, or workspace-write)", source, sandbox)
		}
		if strings.TrimSpace(sandbox) != "inherit" {
			role.Sandbox = strings.TrimSpace(sandbox)
		}
	} else if codexReadOnly(fields["tools"]) {
		role.Sandbox = "read-only"
	}
	return role, nil
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
func renderGlobalAgentTOML(mdPath, raw, agentsDir string, policies ...config.Config) (string, string, error) {
	fields, body, err := claudesource.Fields(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse %s: %w", mdPath, err)
	}
	if len(fields) == 0 {
		return "", "", fmt.Errorf("%s: no frontmatter", mdPath)
	}
	name := strings.TrimSpace(fields["name"])
	description := strings.TrimSpace(fields["description"])
	if name == "" || description == "" {
		return "", "", fmt.Errorf("%s: frontmatter needs both name: and description", mdPath)
	}
	if err := paths.ValidateName(name); err != nil {
		return "", "", fmt.Errorf("%s: %w", mdPath, err)
	}
	body = strings.TrimSpace(body)

	cfg := config.Default()
	if len(policies) > 0 {
		cfg = policies[0]
	}
	ctx := rewrite.Context{Engine: "codex", Kind: "agent", Source: mdPath}
	rules := rewrite.Builtins(rewrite.Func(func(text string, _ rewrite.Context) string {
		return rewriteCodeReview(text, cfg.Machine.ModelMap, cfg.Codex.Review)
	}))
	description = rules.Apply(description, ctx)
	rules = append(rules, rewrite.Configured(cfg.Machine.Rewrites)...)
	rules = append(rules, rewrite.Configured(cfg.Rewrites)...)
	description = rewrite.Configured(append(cfg.Machine.Rewrites, cfg.Rewrites...)).Apply(description, ctx)
	body = rules.Apply(body, ctx)
	role, err := codexRoleSettings(fields, cfg.Machine.ModelMap, mdPath)
	if err != nil {
		return "", "", err
	}

	content := globalRoleHeader(globalAgentMarkerSource(mdPath, agentsDir, cfg.Machine.AgentMarkerDir)) +
		"name = \"" + globalAgentEscape(name) + "\"\n" +
		"description = \"" + globalAgentEscape(description) + "\"\n"
	if role.Model != "" {
		content += "model = \"" + globalAgentEscape(role.Model) + "\"\n"
	}
	if role.Effort != "" {
		content += "model_reasoning_effort = \"" + globalAgentEscape(role.Effort) + "\"\n"
	}
	if role.Sandbox != "" {
		content += "sandbox_mode = \"" + globalAgentEscape(role.Sandbox) + "\"\n"
	}
	content += "developer_instructions = \"\"\"\n" + globalAgentEscapeMultiline(body) + "\n\"\"\"\n"

	return name, content, nil
}

// globalAgentMarkerSource names what the generated marker credits: the clone
// path of an original agent, or variants.json for a variant, whose Markdown
// only ever existed in the engine-sync-owned generated directory. It is deliberately
// clone-relative — a host-absolute path in a marker says nothing an operator
// on another machine can act on.
func globalAgentMarkerSource(mdPath, agentsDir string, markers ...string) string {
	relDir := filepath.Base(agentsDir)
	if len(markers) > 0 && markers[0] != "" {
		relDir = markers[0]
	}
	relDir = filepath.ToSlash(relDir) + "/"
	if filepath.Dir(mdPath) == filepath.Clean(agentsDir) {
		return relDir + filepath.Base(mdPath)
	}
	return relDir + "variants.json"
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
	return renderGlobalAgentTOML(path, raw, dir, cfg...)
}
func ValidateRole(text string) error { return validateTOML(text) }

type Role = codexRole

func RoleSettings(f, m map[string]string, s string) (Role, error) { return codexRoleSettings(f, m, s) }
func EscapeRole(s string) string                                  { return globalAgentEscape(s) }
func EscapeRoleMultiline(s string) string                         { return globalAgentEscapeMultiline(s) }
