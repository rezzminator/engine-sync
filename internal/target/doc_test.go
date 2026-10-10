package target_test

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"testing"
)

func TestTargetsRenderLoadedSourcesWithoutFilesystemAccess(t *testing.T) {
	jail := testjail.Scratch(t)
	root, home := filepath.Join(jail, "project"), filepath.Join(jail, "home")
	for name, body := range map[string]string{"CLAUDE.md": "# Sample\n", ".claude/agents/reader.md": "---\ndescription: Reads a file.\ntools: Read\n---\nRead.\n", ".claude/commands/hello.md": "---\ndescription: Says hello.\n---\nHello.\n", ".mcp.json": "{\"mcpServers\":{\"sample\":{\"command\":\"sample-server\"}}}"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	p := source.LoadProject(root, home, cfg)
	if err := os.Rename(root, root+"-saved"); err != nil {
		t.Fatal(err)
	}
	for _, compiler := range target.Registry {
		artifacts, diagnostics := compiler.Render(p, cfg)
		if len(artifacts) == 0 {
			t.Errorf("%s rendered no artifacts from loaded sources", compiler.Name())
		}
		if len(diagnostics) != 0 {
			t.Errorf("%s diagnostics=%#v", compiler.Name(), diagnostics)
		}
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			t.Errorf("%s touched output root during Render: %v", compiler.Name(), err)
		}
	}
}
