package source

import (
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVariantDeclarationErrorNamesFileAndVariant(t *testing.T) {
	root := testjail.Scratch(t)
	path := filepath.Join(root, "variants.json")
	if err := os.WriteFile(path, []byte(`{"Bad Name":{"from":"reader"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadGlobalAgentVariants(root, filepath.Join(root, "generated"))
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "Bad Name") {
		t.Fatal(err)
	}
}
