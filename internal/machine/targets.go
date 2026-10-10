package machine

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/paths"
	"io/fs"
	"os"
	"path/filepath"
)

// Targets is {engine-sync config}/targets.json: the machine's Codex homes,
// and optionally the Claude config dirs and the source clone.
type Targets struct {
	CodexHomes       *[]string `json:"codexHomes"`
	ClaudeConfigDirs []string  `json:"claudeConfigDirs,omitempty"`
	SourceRepo       string    `json:"sourceRepo,omitempty"`
}

// readTargets returns the targets file, found false when there is none. A
// file that is present but unreadable, unparseable or without codexHomes is
// an error, never an empty roster.
func ReadTargets(get paths.Env, home string) (Targets, bool, error) {
	path := filepath.Join(paths.EngineSyncConfig(get, home), "targets.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Targets{}, false, nil
	}
	if err != nil {
		return Targets{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var targets Targets
	if err := json.Unmarshal(raw, &targets); err != nil {
		return Targets{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	if targets.CodexHomes == nil {
		return Targets{}, false, fmt.Errorf("%s: no \"codexHomes\" list", path)
	}
	for _, dir := range append(append([]string(nil), *targets.CodexHomes...), targets.ClaudeConfigDirs...) {
		if !filepath.IsAbs(dir) {
			return Targets{}, false, fmt.Errorf("%s: %q is not an absolute path", path, dir)
		}
	}
	return targets, true, nil
}
