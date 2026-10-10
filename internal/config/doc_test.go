package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTrackedSchemaMatchesLoader(t *testing.T) {
	raw, e := os.ReadFile(filepath.Join("..", "..", "docs", "engine-sync.schema.json"))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(raw, Schema()) {
		t.Fatal("tracked schema differs from loader's schema")
	}
}
