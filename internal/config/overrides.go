package config

import "fmt"

type CodexOverrides struct {
	ModelMap        map[string]string
	RootAdapter     string
	AgentPreamble   string
	ExcludeDirs     []string
	Projects        []string
	ExcludeProjects []string
	NeverRegister   []string
	SuffixMode      string
	SuffixPrefix    string

	SetRootAdapter   bool
	SetAgentPreamble bool
}

func (cfg Config) WithCodexOverrides(cli CodexOverrides) (Config, error) {
	models := make(map[string]string, len(cfg.Codex.ModelMap))
	for key, value := range cfg.Codex.ModelMap {
		models[key] = value
	}
	cfg.Codex.ModelMap = models
	sources := make(map[string]string, len(cfg.Sources))
	for key, value := range cfg.Sources {
		sources[key] = value
	}
	cfg.Sources = sources
	mark := func(key, flag string) { cfg.Sources["codex."+key] = "flag --" + flag }
	for key, value := range cli.ModelMap {
		cfg.Codex.ModelMap[key] = value
		mark("modelMap."+key, "model")
	}
	if cli.RootAdapter != "" || cli.SetRootAdapter {
		cfg.Codex.RootAdapter = cli.RootAdapter
		mark("rootAdapter", "root-adapter")
	}
	if cli.AgentPreamble != "" || cli.SetAgentPreamble {
		cfg.Codex.AgentPreamble = cli.AgentPreamble
		mark("agentPreamble", "agent-preamble")
	}
	if cli.ExcludeDirs != nil {
		cfg.Codex.ExcludeDirs = append([]string{}, cli.ExcludeDirs...)
		mark("excludeDirs", "exclude-dir")
	}
	if cli.Projects != nil {
		cfg.Codex.Projects = append([]string{}, cli.Projects...)
		mark("projects", "project")
	}
	if cli.ExcludeProjects != nil {
		cfg.Codex.ExcludeProjects = append([]string{}, cli.ExcludeProjects...)
		mark("excludeProjects", "exclude-project")
	}
	if cli.NeverRegister != nil {
		cfg.Codex.NeverRegister = append([]string{}, cli.NeverRegister...)
		mark("neverRegister", "never-register")
	}
	if cli.SuffixMode != "" {
		cfg.Codex.SuffixMode = cli.SuffixMode
		mark("suffixMode", "suffix-mode")
	}
	if cli.SuffixPrefix != "" {
		cfg.Codex.SuffixPrefix = cli.SuffixPrefix
		mark("suffixPrefix", "suffix-prefix")
	}
	if err := Validate(cfg); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", cfg.Source, err)
	}
	return prepareTemplates(cfg)
}
