package source

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"path/filepath"
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

// DeclaredName is the name an agent source declares, the base RoleName applies
// the suffix mode to: its frontmatter `name`, or the file base when the
// source declares none or cannot be read or parsed (a kept link; a header the
// caller reports as FRONTMATTER-INVALID itself).
func DeclaredName(model *Project, path string) string {
	if front, _, err := model.Frontmatter(path); err == nil {
		if name := strings.TrimSpace(front.Fields["name"]); name != "" {
			return name
		}
	}
	return strings.TrimSuffix(filepath.Base(path), ".md")
}

// CommandName is the colon command name of a command file at rel (relative to
// its commands directory): `.md` trimmed, path segments joined with `:`.
func CommandName(rel string) string {
	return strings.ReplaceAll(strings.TrimSuffix(filepath.ToSlash(rel), ".md"), "/", ":")
}
