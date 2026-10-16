package codex

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
)

// defaultGeneratedFileMode is the permission every reconcileFile write has
// always used; artifact.Artifact.Mode overrides it per output.

type reconcileResult reconciler.Result

func codexArtifact(output artifact.Artifact) artifact.Artifact {
	if output.Target != "" {
		output.Kind = artifact.Link
	}
	return output
}

func codexMode(mode Mode) reconciler.Mode {
	if mode == ModeBuild {
		return reconciler.Build
	}
	return reconciler.Check
}

func (r *reconcileResult) reconcileFile(o artifact.Artifact, m Mode, owns func(string) bool) {
	reconciler.FileArtifact((*reconciler.Result)(r), codexArtifact(o), codexMode(m), func(path string) (bool, error) { return owns(path), nil })
}
