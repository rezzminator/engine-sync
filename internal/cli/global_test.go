package cli

import (
	"bytes"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalUsesClaudeConfigSourcesAndPreservesThem(t *testing.T) {
	home, configDir := testjail.Scratch(t), testjail.Scratch(t)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	source := "---\nname: writer\ndescription: Writes git.\ntools: Read, Bash\ncodex-sandbox: inherit\n---\nchildren are Explore+haiku (never\nyour own type)\n"
	putProjectFile(t, configDir, "agents/writer.md", source)
	vars := map[string]string{"HOME": home, "CLAUDE_CONFIG_DIR": configDir}
	e := env{get: func(key string) string { return vars[key] }}
	for _, args := range [][]string{{"global", "--plan"}, {"global"}, {"doctor", "--machine"}, {"codex", "agents"}} {
		var out, errOut bytes.Buffer
		if code := dispatch(args, &out, &errOut, e); code != 0 {
			t.Fatalf("%v rc=%d: %s%s", args, code, &out, &errOut)
		}
	}
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(configDir, alias); err != nil {
		t.Fatal(err)
	}
	var aliasOut, aliasErr bytes.Buffer
	if code := dispatch([]string{"global", "--claude-config-dir", alias}, &aliasOut, &aliasErr, e); code != 0 {
		t.Fatalf("aliased global rc=%d: %s%s", code, &aliasOut, &aliasErr)
	}

	raw, err := os.ReadFile(filepath.Join(configDir, "agents", "writer.md"))
	if err != nil || string(raw) != source {
		t.Fatalf("source changed: %s %v", raw, err)
	}
	raw, err = os.ReadFile(filepath.Join(home, ".codex", "agents", "writer.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "children are Explore+haiku") {
		t.Fatalf("unconfigured global body: %s", raw)
	}
	// The same writer policy is keyed by frontmatter on any agent name.
	putProjectFile(t, configDir, "agents/reader.md", strings.ReplaceAll(source, "name: writer", "name: reader"))
	var out, errOut bytes.Buffer
	if code := dispatch([]string{"global"}, &out, &errOut, e); code != 0 {
		t.Fatalf("global rc=%d: %s%s", code, &out, &errOut)
	}
	raw, err = os.ReadFile(filepath.Join(home, ".codex", "agents", "reader.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "name = \"reader\"") {
		t.Fatalf("frontmatter override on renamed role: %s", raw)
	}
}
