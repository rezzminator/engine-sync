package opencode

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/rewrite"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"strings"
)

// statOpenCodeSource stats one source entry through its symlinks; a dangling
// symlink goes to dangling, any other failure is a problem.
func discoverOpenCodeAgents(dir string, problem func(string, ...any), dangling func(string, error), model *claudesource.Project) []claudesource.Entry {
	var out []claudesource.Entry
	reporter := openCodeReporter(problem, func(path string, err error) {
		dangling(path, err)
		rel, _ := filepath.Rel(dir, path)
		if !strings.Contains(rel, string(filepath.Separator)) && strings.HasSuffix(rel, ".md") && rel != "README.md" {
			out = append(out, claudesource.Entry{Path: path, Rel: rel, Target: model.LinkTarget(path)})
		}
	})
	for _, e := range model.Markdown(dir, nil, reporter) {
		if !e.SkillDir && !strings.Contains(e.Rel, string(filepath.Separator)) {
			out = append(out, claudesource.Entry{Path: e.Path, Rel: e.Rel})
		}
	}
	return out
}

func compileOpenCodeAgents(
	root string,
	projects []string,
	roster map[string]string,
	modelMap map[string]string,
	add func(artifact.Artifact),
	problem, skip, warn func(string, ...any),
	dangling func(string, error),
	model *claudesource.Project,
	policies ...config.Config,
) {
	policy := config.Default()
	if len(policies) > 0 {
		policy = policies[0]
	}
	seen := map[string]claudesource.Entry{}
	var kept []string
	knownServers := knownMCPServers(model, root, policy.MCP)
	for _, project := range projects {
		for _, entry := range discoverOpenCodeAgents(filepath.Join(root, project, ".claude", "agents"), problem, dangling, model) {
			base := strings.TrimSuffix(filepath.Base(entry.Path), ".md")
			name, register, err := claudesource.RoleName(base, project, policy.Codex)
			if err != nil {
				problem("%s: %v", entry.Path, err)
				continue
			}
			if !register {
				warn("skip agent %s — never-register policy", entry.Path)
				continue
			}
			key := paths.FoldName(name)
			if previous, exists := seen[key]; exists {
				problem("%s role name %s from %s and %s", reconcile.Conflict, name, previous.Path, entry.Path)
				continue
			}
			seen[key] = entry

			if entry.Target != "" {
				kept = append(kept, name)
				continue
			}
			compileOpenCodeAgent(root, name, entry.Path, roster, modelMap, knownServers, add, problem, warn, model, policy)
		}
	}
	for _, name := range kept {
		if entry := seen[paths.FoldName(name)]; entry.Target != "" {
			add(artifact.Artifact{Path: filepath.Join(root, ".opencode", "agent", name+".md"), Kept: &artifact.SourceLink{Path: entry.Path, Target: entry.Target}})
		}
	}
}

func compileOpenCodeAgent(
	root, name, path string,
	roster map[string]string,
	modelMap map[string]string,
	knownServers []string,
	add func(artifact.Artifact),
	skip, warn func(string, ...any),
	model *claudesource.Project,
	policies ...config.Config,
) {
	policy := config.Default()
	if len(policies) > 0 {
		policy = policies[0]
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		skip("relative path of %s: %v", path, err)
		return
	}
	_, err = model.Read(path)
	if err != nil {
		skip("read %s: %v", path, err)
		return
	}
	front, body, parseErr := model.Frontmatter(path)
	fields := front.Fields
	if parseErr != nil {
		skip("parse %s: %v", path, parseErr)
		return
	}
	ctx := rewrite.Context{Engine: "opencode", Kind: "agent", Source: path, Roster: roster}
	chain := append(rewrite.Builtins(), rewrite.Configured(policy.Rewrites)...)
	mode := openCodeAgentMode
	if value := front.Engine["opencode"]["mode"]; value != "" {
		mode = value
	}
	description := chain.Apply(strings.TrimSpace(fields["description"]), ctx)
	content := "---\n" + artifact.Marker(artifact.YAMLFront,
		filepath.ToSlash(rel),
	) + "\ndescription: " + quoteOpenCodeYAML(
		description,
	) + "\nmode: " + mode + "\n"
	alias := strings.TrimSpace(fields["model"])
	if comment := strings.Index(alias, " #"); comment >= 0 {
		alias = strings.TrimSpace(alias[:comment])
	}
	if pin := strings.TrimSpace(front.Engine["opencode"]["model"]); pin != "" {
		content += "model: " + quoteOpenCodeYAML(pin) + "\n"
	} else if alias != "" && alias != "inherit" {
		model, ok := modelMap[alias]
		if !ok || strings.TrimSpace(model) == "" {
			warn("unmapped OpenCode model alias %q in %s — model omitted", alias, path)
		} else {
			content += "model: " + quoteOpenCodeYAML(strings.TrimSpace(model)) + "\n"
		}
	}
	if tools, ok := fields["tools"]; ok {
		toolsBlock, toolsWarnings, toolsErr := renderOpenCodeToolsBlock(path, tools, knownServers, policy.MCP)
		if toolsErr != nil {
			skip("tools %s: %v", path, toolsErr)
			return
		}
		for _, toolsWarning := range toolsWarnings {
			warn("%s", toolsWarning)
		}
		content += toolsBlock
	}
	if agent, ok := policy.OpenCode.Agent[name]; ok {
		content += renderAgentPermission(agent.Permission)
	}
	preamble, err := config.RenderTemplate(policy.OpenCode.AgentPreamble, config.TemplateData{Name: name, Engine: "OpenCode", Source: filepath.ToSlash(rel), Root: "AGENTS.md"})
	if err != nil {
		skip("render opencode.agentPreamble for %s: %v", path, err)
		return
	}
	content += "---\n" + preamble + chain.Apply(strings.TrimSpace(body), ctx) + "\n"
	add(artifact.Artifact{Path: filepath.Join(root, ".opencode", "agent", name+".md"), Content: []byte(content), NativeMode: mode, Source: path, Syntax: artifact.YAMLFront})
}

func selectedPolicy(policies []config.OpenCode) config.OpenCode {
	if len(policies) > 0 {
		return policies[0]
	}
	return config.Default().OpenCode
}
