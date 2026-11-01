package codex

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/paths"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
)

// projectDocMaxBytes is Codex's default project_doc_max_bytes: the bytes of
// the AGENTS.md chain, repository root down to the working directory, it
// reads before cutting the rest.
const projectDocMaxBytes = 32 << 10

// agentsBudget names every compiled AGENTS.md whose chain, it and the
// AGENTS.md of each project above it, passes the project's
// project_doc_max_bytes: a warning in a build, which writes it whole, and a
// failure in check, never a cut. An unreadable limit is an error.
func agentsBudget(root, home string, cfg config.Config, orientations []artifact.Artifact) ([]diag.Diagnostic, error) {
	if len(orientations) == 0 {
		return nil, nil
	}
	limit, err := projectDocLimit(root, home, cfg)
	if err != nil || limit == 0 {
		return nil, err // a limit of 0 turns Codex's project docs off
	}
	dir := func(a artifact.Artifact) string { return filepath.Dir(a.Path) }
	var diagnostics []diag.Diagnostic
	for _, document := range orientations {
		total, parts := 0, []string{}
		for _, ancestor := range orientations {
			if rel, err := filepath.Rel(dir(ancestor), dir(document)); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			total += len(ancestor.Content)
			name, _ := filepath.Rel(root, ancestor.Path)
			parts = append(parts, fmt.Sprintf("%s %d", filepath.ToSlash(name), len(ancestor.Content)))
		}
		if total <= limit {
			continue
		}
		diagnostics = append(diagnostics, diag.Diagnostic{
			Severity: diag.Warning, State: reconciler.OverBudget, Path: document.Path,
			Message: fmt.Sprintf("AGENTS-BUDGET %s: its Codex chain is %d > %d bytes (%s); Codex reads none past the limit", document.Path, total, limit, strings.Join(parts, " + ")),
			Remedy:  "shorten CLAUDE.md or an always-on project rule, give a rule `paths:`, or raise project_doc_max_bytes in .codex/config.toml",
		})
	}
	return diagnostics, nil
}

// projectDocLimit is the project_doc_max_bytes Codex applies: the project's
// .codex/config.toml, which wins, else the strictest of the Codex homes'
// config.toml, each home without the key at Codex's default; 0 when every home
// turns project docs off. A limit below 0 is invalid, as Codex reads it.
func projectDocLimit(root, home string, cfg config.Config) (int, error) {
	if limit, set, err := readProjectDocLimit(filepath.Join(root, ".codex", "config.toml")); err != nil || set {
		return limit, err
	}
	roster, err := config.ResolveTargets(paths.Getenv, home, cfg, config.Targets{})
	if err != nil {
		return 0, fmt.Errorf("AGENTS-BUDGET resolve the Codex homes: %w", err)
	}
	if roster.CodexHomes == nil || len(*roster.CodexHomes) == 0 {
		return projectDocMaxBytes, nil
	}
	strictest := 0
	for _, codexHome := range *roster.CodexHomes {
		limit, set, err := readProjectDocLimit(filepath.Join(codexHome, "config.toml"))
		if err != nil {
			return 0, err
		}
		if !set {
			limit = projectDocMaxBytes
		}
		if limit > 0 && (strictest == 0 || limit < strictest) {
			strictest = limit
		}
	}
	return strictest, nil
}

// readProjectDocLimit is path's project_doc_max_bytes; set is false when the
// file or the key is absent.
func readProjectDocLimit(path string) (limit int, set bool, err error) {
	raw, err := paths.ReadRegular(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("AGENTS-BUDGET read project_doc_max_bytes from %s: %w", path, err)
	}
	var document struct {
		ProjectDocMaxBytes *int `toml:"project_doc_max_bytes"`
	}
	if _, err := toml.Decode(string(raw), &document); err != nil {
		return 0, false, fmt.Errorf("AGENTS-BUDGET read project_doc_max_bytes from %s: %w", path, err)
	}
	if document.ProjectDocMaxBytes == nil {
		return 0, false, nil
	}
	if *document.ProjectDocMaxBytes < 0 {
		return 0, false, fmt.Errorf("AGENTS-BUDGET project_doc_max_bytes in %s is %d: it must be 0 or more", path, *document.ProjectDocMaxBytes)
	}
	return *document.ProjectDocMaxBytes, true, nil
}
