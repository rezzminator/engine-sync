package rewrite

import (
	"testing"
)

func TestChainOrdersAndScopesRules(t *testing.T) {
	for _, row := range []struct{ engine, kind, want string }{
		{"codex", "agent", "selected $hello final"},
		{"opencode", "agent", "selected /hello alias"},
		{"codex", "command", "selected $hello alias"},
	} {
		prefix := "$"
		if row.engine == "opencode" {
			prefix = "/"
		}
		ctx := Context{Engine: row.engine, Kind: row.kind, ModelMap: map[string]string{"alias": "selected"}, Roster: map[string]string{"tools:hello": prefix + "hello"}}
		chain := Builtins(Func(func(text string, _ Context) string { return text + " alias" }))
		chain = append(chain, Literal{Engines: []string{"codex"}, Kinds: []string{"agent"}, Text: "hello alias", Replace: "hello final"})
		if got := chain.Apply("alias /tools:hello", ctx); got != row.want {
			t.Fatalf("%s/%s=%q want %q", row.engine, row.kind, got, row.want)
		}
	}
}
