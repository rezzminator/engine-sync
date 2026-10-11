package rewrite

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"testing"
)

func TestConfiguredRewritesScopeAndOrder(t *testing.T) {
	chain := Configured([]config.Rewrite{{Engines: []string{"codex"}, Literal: "old", Replace: "middle"}, {Kinds: []string{"agent"}, Literal: "middle", Replace: "new"}})
	if got := chain.Apply("old", Context{Engine: "codex", Kind: "agent"}); got != "new" {
		t.Fatal(got)
	}
	if got := chain.Apply("old", Context{Engine: "opencode", Kind: "command"}); got != "old" {
		t.Fatal(got)
	}
}
