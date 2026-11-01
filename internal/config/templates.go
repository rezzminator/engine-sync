package config

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"text/template"
)

// TemplateData is the complete public template field set. Models is the
// engine's model map, so prose can name a model as {{index .Models "alias"}}.
type TemplateData struct {
	Name, Engine, Source, Root string
	Models                     map[string]string
}

func RenderTemplate(text string, data TemplateData) (string, error) {
	return renderTemplateReporting(text, data, func(string) {})
}

// renderTemplateReporting replaces the template builtin index for the model map: an
// alias the map lacks renders as the alias itself and is passed to missing,
// never blank and never a failed render.
func renderTemplateReporting(text string, data TemplateData, missing func(alias string)) (string, error) {
	index := func(models map[string]string, alias string) string {
		model, ok := models[alias]
		if !ok {
			missing(alias)
			return alias
		}
		return model
	}
	t, e := template.New("adapter").Option("missingkey=error").Funcs(template.FuncMap{"index": index}).Parse(text)
	if e != nil {
		return "", e
	}
	var out bytes.Buffer
	e = t.Execute(&out, data)
	return out.String(), e
}

// addWarning records notice once.
func addWarning(cfg *Config, notice string) {
	if !slices.Contains(cfg.Warnings, notice) {
		cfg.Warnings = append(cfg.Warnings, notice)
	}
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
			addWarning(&cfg, row.key+": ${name} is deprecated; use {{.Name}}")
		}
		missing := func(alias string) {
			addWarning(&cfg, fmt.Sprintf("config %s: %s: model alias %q is not in the engine's modelMap; it renders as %q", cfg.Sources[row.key], row.key, alias, alias))
		}
		if _, e := renderTemplateReporting(*row.text, TemplateData{Models: row.models}, missing); e != nil {
			return Config{}, fmt.Errorf("config %s: %s: %w", cfg.Sources[row.key], row.key, e)
		}
	}
	if cfg.Machine.ClaudeAgentsDir == "" {
		cfg.Machine.ClaudeAgentsDir = cfg.ClaudeConfigDir + "/agents"
	}
	return cfg, nil
}
