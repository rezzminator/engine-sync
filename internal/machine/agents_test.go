package machine

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineUnmappedAliasProducesUnpinnedRole(t *testing.T) {
	home := testjail.Scratch(t)
	repo := filepath.Join(home, "clone")
	path := filepath.Join(repo, "templates", "global", "agents", "reader.md")
	writeAgentFixture(t, path, "---\nname: reader\ndescription: Reads.\nmodel: unknown\n---\nRead.\n")
	cfg, err := config.Resolve("", home, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	r, e := RunGlobalAgents(GlobalAgentsOptions{Home: home, SourceRepo: repo, Config: &cfg})
	if e != nil || len(r.Problems) > 0 {
		t.Fatalf("result=%#v err=%v", r, e)
	}
	content := string(readAgentFixture(t, filepath.Join(home, ".codex", "agents", "reader.toml")))
	if strings.Contains(content, "\nmodel =") {
		t.Fatalf("unmapped alias pinned: %s", content)
	}
}
