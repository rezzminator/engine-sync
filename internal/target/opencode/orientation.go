package opencode

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"io/fs"
	"path/filepath"
	"strings"
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

// agentsDrift checks the AGENTS.md OpenCode reads, which the Codex build
// writes: an engine-sync AGENTS.md that differs from a fresh compile of its
// CLAUDE.md and project rules is STALE, a warning in a build and a failure in
// check and doctor. An absent or hand-written one is the Codex check's to
// report; one that cannot be compiled or read is a warning, never clean, and
// leaves every other AGENTS.md checked.
func agentsDrift(p claudesource.Project, cfg config.Config) []diag.Diagnostic {
	const remedy = "OpenCode reads it; run engine-sync codex build"
	orientations, problems := codex.Orientations(p, cfg)
	var diagnostics []diag.Diagnostic
	if len(problems) != 0 {
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, Message: fmt.Sprintf("AGENTS.md unchecked: it does not compile: %s", strings.Join(problems, "; ")), Remedy: "run engine-sync codex check"})
	}
	for _, want := range orientations {
		if want.Kept != nil {
			continue // it did not compile: the problems above name it
		}
		have, err := paths.ReadRegular(want.Path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, Path: want.Path, Message: fmt.Sprintf("AGENTS.md unchecked: inspect %s: %v", want.Path, err)})
			continue
		}
		if bytes.Equal(have, want.Content) || !artifact.Claims(string(have)) {
			continue
		}
		diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Warning, State: reconcile.Stale, Path: want.Path, Message: fmt.Sprintf("%s %s", reconcile.Stale, want.Path), Remedy: remedy})
	}
	return diagnostics
}
