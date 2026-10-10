package artifact_test

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactModeIsPublished(t *testing.T) {
	path := filepath.Join(testjail.Scratch(t), "role.toml")
	r := reconcile.Run([]artifact.Artifact{{Path: path, Mode: 0600, Content: []byte(artifact.Marker(artifact.TOML, "source"))}}, reconcile.Build, nil)
	info, err := os.Stat(path)
	if r.Wrote != 1 || err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("%+v %v", r, err)
	}
}
