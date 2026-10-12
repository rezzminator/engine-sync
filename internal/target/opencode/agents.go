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
			name, register, err := claudesource.RoleName(claudesource.DeclaredName(model, entry.Path), project, policy.Codex)
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
	if parseErr != nil {
		skip("%s", claudesource.DescribeParseError(path, parseErr))
		return
	}
	preamble, err := config.RenderTemplate(policy.OpenCode.AgentPreamble, config.TemplateData{Name: name, Engine: "OpenCode", Source: filepath.ToSlash(rel), Root: "AGENTS.md", Models: modelMap})
	if err != nil {
		skip("render opencode.agentPreamble for %s: %v", path, err)
		return
	}
	renderOpenCodeAgent(openCodeAgentSpec{
		Name: name, Path: path, Marker: filepath.ToSlash(rel), Preamble: preamble,
		Output: filepath.Join(root, ".opencode", "agent", name+".md"),
	}, front, body, roster, modelMap, knownServers, policy.Rewrites, add, skip, warn, policy)
}

// openCodeAgentSpec is what differs between a project agent and a
// machine-global one: its name, source, marker credit, preamble and output.
type openCodeAgentSpec struct {
	Name, Path, Marker, Preamble, Output string
	Generated                            bool
}

// renderOpenCodeAgent is the one OpenCode agent renderer. An agent whose
// requires the host does not meet is SKIPPED; `engines.opencode: false` is not
// emitted.
func renderOpenCodeAgent(
	spec openCodeAgentSpec,
	front claudesource.Frontmatter,
	body string,
	roster, modelMap map[string]string,
	knownServers []string,
	rewrites []config.Rewrite,
	add func(artifact.Artifact),
	skip, warn func(string, ...any),
	policy config.Config,
) {
	problems, warnings := claudesource.ValidateAgent(spec.Path, front, policy.Agents.PassthroughKeys, spec.Generated)
	for _, w := range warnings {
		warn("%s", w)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			skip("%s", p)
		}
		return
	}
	if missing := claudesource.Unmet(front.Requires, policy.Capabilities); missing != "" {
		warn("%s", claudesource.Skipped(spec.Name, missing))
		return
	}
	if front.Engines.OpenCodeOff() {
		return
	}
	settings := claudesource.OpenCodeEngine{}
	if front.Engines.OpenCode != nil {
		settings = *front.Engines.OpenCode
	}
	ctx := rewrite.Context{Engine: "opencode", Kind: "agent", Source: spec.Path, Roster: roster}
	chain := append(rewrite.Builtins(), rewrite.Configured(rewrites)...)
	mode := openCodeAgentMode
	if settings.Mode != "" {
		mode = settings.Mode
	}
	description := chain.Apply(strings.TrimSpace(front.Fields["description"]), ctx)
	content := "---\n" + artifact.Marker(artifact.YAMLFront, spec.Marker) + "\ndescription: " + quoteOpenCodeYAML(description) + "\nmode: " + mode + "\n"
	alias := strings.TrimSpace(front.Model)
	if pin := strings.TrimSpace(settings.Model); pin != "" {
		if mapped := strings.TrimSpace(modelMap[pin]); mapped != "" {
			pin = mapped
		}
		content += "model: " + quoteOpenCodeYAML(pin) + "\n"
	} else if alias != "" && alias != "inherit" {
		mapped, ok := modelMap[alias]
		if !ok || strings.TrimSpace(mapped) == "" {
			warn("unmapped OpenCode model alias %q in %s — model omitted", alias, spec.Path)
		} else {
			content += "model: " + quoteOpenCodeYAML(strings.TrimSpace(mapped)) + "\n"
		}
	}
	if tools, ok := front.Fields["tools"]; ok {
		toolsBlock, toolsWarnings, toolsErr := renderOpenCodeToolsBlock(spec.Path, tools, knownServers, policy.MCP)
		if toolsErr != nil {
			skip("tools %s: %v", spec.Path, toolsErr)
			return
		}
		for _, toolsWarning := range toolsWarnings {
			warn("%s", toolsWarning)
		}
		content += toolsBlock
	}
	if settings.Permission != nil {
		content += renderAgentPermission(*settings.Permission)
	}
	content += "---\n" + spec.Preamble + chain.Apply(strings.TrimSpace(body), ctx) + "\n"
	add(artifact.Artifact{Path: spec.Output, Content: []byte(content), NativeMode: mode, Source: spec.Path, Syntax: artifact.YAMLFront})
}

func selectedPolicy(policies []config.OpenCode) config.OpenCode {
	if len(policies) > 0 {
		return policies[0]
	}
	return config.Default().OpenCode
}
