package codex

import (
	"github.com/rezzminator/engine-sync/internal/config"
)

const suffixModeNone = "none"

const suffixModeStripPrefix = "strip-prefix"

type Config = config.Codex

type CLIOverrides = config.CodexOverrides

func defaultConfig() Config { return config.Default().Codex }

func CodexDefaults() Config { return defaultConfig() }

func ReadCodexConfig(root string, cli CLIOverrides) (Config, error) { return loadConfig(root, cli) }

func loadConfig(root string, cli CLIOverrides) (Config, error) {
	cfg, err := config.LoadConfigLayers(root)
	if err != nil {
		return Config{}, err
	}
	cfg, err = cfg.WithCodexOverrides(cli)
	return cfg.Codex, err
}
