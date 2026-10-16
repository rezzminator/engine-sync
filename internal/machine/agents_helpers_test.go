package machine

import (
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAgentFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readAgentFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func parseFrontmatter(text string) (map[string]string, string, error) {
	front, body, err := source.Parse(text)
	return front.Fields, body, err
}

var rewriteCodeReview = codex.RewriteReview

func assertAgentContains(t *testing.T, path string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if !strings.Contains(string(readAgentFixture(t, path)), text) {
			t.Fatalf("%s misses %q", path, text)
		}
	}
}

func fixtureMachinePolicy() config.Config {
	c := config.Default()
	c.Codex.ModelMap = map[string]string{"opus": "gpt-6.1-sol", "sonnet": "gpt-6-luna", "haiku": "gpt-6-luna", "fable": "gpt-6.1-sol"}
	c.Codex.Review = config.Review{ModelAlias: "opus", DefaultEffort: "low", Prompt: "Review the current task files.", SlotPrompt: "Review the selected flight files."}
	return c
}

func runFixtureMachineAgents(o GlobalAgentsOptions) (GlobalAgentsResult, error) {
	if o.Config == nil {
		c, e := config.LoadConfigLayers("", o.Home)
		if e != nil {
			return GlobalAgentsResult{}, e
		}
		if len(c.Codex.ModelMap) == 0 {
			c.Codex.ModelMap = fixtureMachinePolicy().Codex.ModelMap
		}
		c.Codex.Review = fixtureMachinePolicy().Codex.Review
		if c.Machine.SourceGeneratedAgentsDir == "" {
			c.Machine.SourceGeneratedAgentsDir = "generated/agents"
		}
		o.Config = &c
	}
	return RunGlobalAgents(o)
}

func renderFixtureMachineAgent(path, raw, dir string, configs ...config.Config) (string, string, error) {
	if len(configs) == 0 {
		configs = []config.Config{fixtureMachinePolicy()}
	}
	return codex.RenderMachineAgent(path, raw, dir, configs...)
}
