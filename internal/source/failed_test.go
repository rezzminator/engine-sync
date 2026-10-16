package source

import (
	"errors"
	"slices"
	"testing"
)

// A declaration error that carries its own code keeps it; any other is
// VARIANT-INVALID. A broken source is kept under its file name and the
// name its header line gives.
func TestFailedDeclarationAndSourceNames(t *testing.T) {
	if got := declarationProblem(errors.New("UNSET-MISSING x: lead.md has no frontmatter key \"k\"")); got != "UNSET-MISSING x: lead.md has no frontmatter key \"k\"" {
		t.Fatalf("coded = %q", got)
	}
	if got := declarationProblem(errors.New("v.json: variant \"x\": bad")); got != "VARIANT-INVALID v.json: variant \"x\": bad" {
		t.Fatalf("uncoded = %q", got)
	}
	if got := failedSourceNames("/a/file.md", []byte("---\nname: role\n\tbroken: x\n---\n")); !slices.Equal(got, []string{"file", "role"}) {
		t.Fatalf("names = %q", got)
	}
}
