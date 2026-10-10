package reconcile_test

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestMachineLinkRepairsMissingTarget(t *testing.T) {
	root := testjail.Scratch(t)
	source := filepath.Join(root, "source.md")
	if err := os.WriteFile(source, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.md")
	state, _, err := reconcile.ClassifyGlobalLink(target, source, root, reconcile.GlobalLinkFile)
	if err != nil || state != reconcile.GlobalLinkMissing {
		t.Fatal(state, err)
	}
	result := reconcile.Run([]artifact.Artifact{{Path: target, Target: source, Kind: artifact.Link}}, reconcile.Build, []artifact.Root{{Path: root}})
	if len(result.Problems) > 0 {
		t.Fatal(result.Problems)
	}
	state, _, err = reconcile.ClassifyGlobalLink(target, source, root, reconcile.GlobalLinkFile)
	if err != nil || state != reconcile.GlobalLinkCorrect {
		t.Fatal(state, err)
	}
}
