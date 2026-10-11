package config

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"io"
	"path/filepath"
)

type CachedResult struct {
	Config *Config
	Err    error
}

func ResolveCached(root, home string, get paths.Env, policies map[string]CachedResult, notices map[string]bool, stderr io.Writer) (*Config, error) {
	if notices == nil {
		notices = map[string]bool{}
	}
	key := home + "\x00" + root
	if result, found := policies[key]; found {
		return result.Config, result.Err
	}
	cfg, err := Resolve(root, home, get)
	if policies != nil {
		policies[key] = CachedResult{Config: &cfg, Err: err}
	}
	if err != nil {
		return nil, err
	}
	for _, notice := range cfg.Deprecations {
		if !notices[notice] {
			fmt.Fprintln(stderr, "engine-sync: "+notice)
			notices[notice] = true
		}
	}
	if cfg.Legacy && !notices[cfg.Source] {
		fmt.Fprintf(stderr, "engine-sync: %s is deprecated; use %s\n", cfg.Source, filepath.Join(root, RelPath))
		if notices != nil {
			notices[cfg.Source] = true
		}
	}
	if policies != nil {
		policies[key] = CachedResult{Config: &cfg}
	}
	return &cfg, nil
}
