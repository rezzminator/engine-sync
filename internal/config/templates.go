package config

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// TemplateData is the complete public template field set. Models is the
// engine's model map, so prose can name a model as {{index .Models "alias"}}.
type TemplateData struct {
	Name, Engine, Source, Root string
	Models                     map[string]string
}

// strictIndex replaces the template builtin for the model map: an alias the
// map lacks fails the render, naming it, never renders blank.
func strictIndex(models map[string]string, alias string) (string, error) {
	model, ok := models[alias]
	if !ok {
		return "", fmt.Errorf("model alias %q is not in the engine's modelMap", alias)
	}
	return model, nil
}

func RenderTemplate(text string, data TemplateData) (string, error) {
	t, e := template.New("adapter").Option("missingkey=error").Funcs(template.FuncMap{"index": strictIndex}).Parse(text)
	if e != nil {
		return "", e
	}
	var out bytes.Buffer
	e = t.Execute(&out, data)
	return out.String(), e
}
func prepareTemplates(cfg Config) (Config, error) {
	rows := []struct {
		key    string
		text   *string
		models map[string]string
	}{{"codex.agentPreamble", &cfg.Codex.AgentPreamble, cfg.Codex.ModelMap}, {"codex.rootAdapter", &cfg.Codex.RootAdapter, cfg.Codex.ModelMap}, {"opencode.agentPreamble", &cfg.OpenCode.AgentPreamble, cfg.OpenCode.ModelMap}}
	for _, row := range rows {
		if row.key != "codex.rootAdapter" && strings.Contains(*row.text, "${name}") {
			*row.text = strings.ReplaceAll(*row.text, "${name}", "{{.Name}}")
			notice := row.key + ": ${name} is deprecated; use {{.Name}}"
			seen := false
			for _, n := range cfg.Deprecations {
				seen = seen || n == notice
			}
			if !seen {
				cfg.Deprecations = append(cfg.Deprecations, notice)
			}
		}
		if _, e := RenderTemplate(*row.text, TemplateData{Models: row.models}); e != nil {
			return Config{}, fmt.Errorf("config %s: %s: %w", cfg.Sources[row.key], row.key, e)
		}
	}
	if cfg.Machine.ClaudeAgentsDir == "" {
		cfg.Machine.ClaudeAgentsDir = cfg.ClaudeConfigDir + "/agents"
	}
	return cfg, nil
}
