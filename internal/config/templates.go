package config

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// TemplateData is the complete public template field set.
type TemplateData struct{ Name, Engine, Source, Root string }

func RenderTemplate(text string, data TemplateData) (string, error) {
	t, e := template.New("adapter").Option("missingkey=error").Parse(text)
	if e != nil {
		return "", e
	}
	var out bytes.Buffer
	e = t.Execute(&out, data)
	return out.String(), e
}
func prepareTemplates(cfg Config) (Config, error) {
	rows := []struct {
		key  string
		text *string
	}{{"codex.agentPreamble", &cfg.Codex.AgentPreamble}, {"codex.rootAdapter", &cfg.Codex.RootAdapter}, {"opencode.agentPreamble", &cfg.OpenCode.AgentPreamble}}
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
		if _, e := RenderTemplate(*row.text, TemplateData{}); e != nil {
			return Config{}, fmt.Errorf("config %s: %s: %w", cfg.Sources[row.key], row.key, e)
		}
	}
	if cfg.Machine.ClaudeAgentsDir == "" {
		cfg.Machine.ClaudeAgentsDir = cfg.ClaudeConfigDir + "/agents"
	}
	return cfg, nil
}
