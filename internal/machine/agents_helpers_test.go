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

var parseFrontmatter = source.Fields

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
	c.Machine.ModelMap = c.Codex.ModelMap
	return c
}

func runFixtureMachineAgents(o GlobalAgentsOptions) (GlobalAgentsResult, error) {
	if o.Config == nil {
		c, e := config.LoadConfigLayers("", o.Home)
		if e != nil {
			return GlobalAgentsResult{}, e
		}
		if len(c.Machine.ModelMap) == 0 {
			c.Machine.ModelMap = fixtureMachinePolicy().Machine.ModelMap
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
