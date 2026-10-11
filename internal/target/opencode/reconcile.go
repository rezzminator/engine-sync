package opencode

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
)

func openCodeArtifact(output artifact.Artifact) artifact.Artifact {
	if output.Target != "" {
		output.Kind = artifact.Link
	}
	return output
}
func openCodeMode(mode Mode) reconcile.Mode {
	if mode == ModeBuild {
		return reconcile.Build
	}
	return reconcile.Check
}
