package opencode

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
)

type reconcileResult = reconcile.Result

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

func reconcileOpenCodeLink(r *reconcileResult, o artifact.Artifact, m Mode) {
	reconcile.LinkArtifact(r, openCodeArtifact(o), openCodeMode(m), isClaimable)
}

func reconcileOpenCodeFile(r *reconcileResult, o artifact.Artifact, m Mode) {
	reconcile.FileArtifact(r, openCodeArtifact(o), openCodeMode(m), isClaimable)
}

func reconcileOpenCodeOrphans(r *reconcileResult, dir string, wanted map[string]bool, m Mode) {
	reconcile.Orphans(r, dir, wanted, openCodeMode(m), isClaimable)
}

func isClaimable(path string) (bool, error) { return reconcile.Claimable(path, nil) }
