package source

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"slices"
	"strings"
)

// RoleName applies shared registration policy before either target emits paths.
func RoleName(base, project string, cfg config.Codex) (string, bool, error) {
	name := base
	if project != "." {
		suffix := project
		switch cfg.SuffixMode {
		case "none":
			suffix = ""
		case "strip-prefix":
			suffix = strings.TrimPrefix(project, cfg.SuffixPrefix)
		}
		if suffix == "" && cfg.SuffixMode != "none" {
			return "", false, fmt.Errorf("project %s has an empty agent suffix under %s policy", project, cfg.SuffixMode)
		}
		if suffix != "" {
			name += "-" + strings.ReplaceAll(suffix, "/", "-")
		}
	}
	if err := paths.ValidateName(name); err != nil {
		return "", false, err
	}
	return name, !slices.Contains(cfg.NeverRegister, base) && !slices.Contains(cfg.NeverRegister, name), nil
}
