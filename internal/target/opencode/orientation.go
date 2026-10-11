package opencode

import (
	"errors"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"io/fs"
	"path/filepath"
)

func compileOpenCodeMirrors(root string, model *claudesource.Project, cfg config.Config, add func(artifact.Artifact), problem, warn func(string, ...any)) {
	for _, name := range cfg.OpenCode.MirrorFiles {
		source := filepath.Join(root, name)
		if content, readErr := model.Read(source); readErr == nil {
			add(artifact.Artifact{Path: filepath.Join(root, ".opencode", name), Content: []byte(string(content)), Source: source})
		} else if !errors.Is(readErr, fs.ErrNotExist) {
			problem("read %s: %v", source, readErr)
		} else {
			warn("missing %s — nothing to mirror into .opencode/%s", source, name)
		}
	}

}
