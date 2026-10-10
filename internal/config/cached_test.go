package config

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestCachedFailureAndNotice(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	p := filepath.Join(root, filepath.FromSlash(RelPath))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{bad`), 0600); err != nil {
		t.Fatal(err)
	}
	cache := map[string]CachedResult{}
	var out bytes.Buffer
	get := func(string) string { return "" }
	_, first := ResolveCached(root, home, get, cache, nil, &out)
	if first == nil {
		t.Fatal("invalid policy accepted")
	}
	if err := os.WriteFile(p, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, again := ResolveCached(root, home, get, cache, nil, &out)
	if again == nil || again.Error() != first.Error() {
		t.Fatalf("failed lookup lost: %v", again)
	}
	if _, err := ResolveCached(root, home, get, nil, nil, &out); err != nil {
		t.Fatal(err)
	}
}
