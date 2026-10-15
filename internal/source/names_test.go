package source

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/testjail"
)

func TestRoleNamePolicy(t *testing.T) {
	cfg := config.Default().Codex
	cfg.SuffixMode = "strip-prefix"
	cfg.SuffixPrefix = "pkg/"
	cfg.NeverRegister = []string{"reader-secret"}
	for _, tc := range []struct {
		base, project, want string
		register            bool
	}{{"reader", "pkg/api", "reader-api", true}, {"reader", "pkg/secret", "reader-secret", false}} {
		got, register, err := RoleName(tc.base, tc.project, cfg)
		if err != nil || got != tc.want || register != tc.register {
			t.Errorf("%#v got %s/%v %v", tc, got, register, err)
		}
	}
	if _, _, err := RoleName("../escape", ".", cfg); err == nil {
		t.Fatal("path name admitted")
	}
}

// TestDeclaredName: the frontmatter name wins over the file base; a source
// without one, or one that fails to parse, falls back to the base.
func TestDeclaredName(t *testing.T) {
	dir := testjail.Scratch(t)
	for file, tc := range map[string]struct{ content, want string }{
		"lead-file.md": {content: "---\nname: lead\n---\nLead.\n", want: "lead"},
		"plain.md":     {content: "---\ndescription: No name.\n---\nPlain.\n", want: "plain"},
		"broken.md":    {content: "---\nname: lead\nengines:\n  codex: [\n---\nBroken.\n", want: "broken"},
	} {
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := DeclaredName(nil, path); got != tc.want {
			t.Errorf("DeclaredName(%s) = %q, want %q", file, got, tc.want)
		}
	}
}

func TestCommandName(t *testing.T) {
	for rel, want := range map[string]string{
		"flights/spec.md":   "flights:spec",
		"pfm.md":            "pfm",
		"quality/prompt.md": "quality:prompt",
	} {
		if got := CommandName(rel); got != want {
			t.Errorf("CommandName(%q) = %q, want %q", rel, got, want)
		}
	}
}
