package source

import (
	"strings"
	"testing"
)

func TestVariantRewritePreservesUnselectedFrontmatter(t *testing.T) {
	got, err := RenderVariant("---\nname: original\nmodel: alias\ncustom: keep\n---\nUse original body.\n", "variant", "source", map[string]string{"model": "chosen"}, map[string]string{"original body": "changed body"})
	if err != nil || !strings.Contains(got, "custom: keep") || !strings.Contains(got, "name: variant") || !strings.Contains(got, "Use changed body.") {
		t.Fatalf("%q %v", got, err)
	}
}
