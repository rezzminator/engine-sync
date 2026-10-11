package cli

import (
	"bytes"
	"fmt"
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

func TestReviewMachineRosterAndConflicts(t *testing.T) {
	for _, name := range []string{"policy homes", "targets homes", "env homes", "conflict", "empty source", "invalid name"} {
		t.Run(name, func(t *testing.T) {
			home := testjail.Scratch(t)
			chosen := filepath.Join(home, "chosen")
			vars := map[string]string{"HOME": home}
			e := env{get: func(k string) string { return vars[k] }}
			putProjectFile(t, home, ".claude/agents/reader.md", "---\nname: reader\ndescription: Reads.\ntools: Read\n---\nRead.\n")
			switch name {
			case "policy homes":
				putProjectFile(t, home, ".config/engine-sync/config.json", `{"version":1,"machine":{"codexHomes":["`+chosen+`"]}}`)
			case "targets homes":
				putProjectFile(t, home, ".config/engine-sync/targets.json", `{"codexHomes":["`+chosen+`"]}`)
			case "env homes":
				vars["CODEX_HOME"] = chosen
			case "conflict":
				putProjectFile(t, home, ".codex/agents/reader.toml", "user role")
			case "empty source":
				if e := os.Remove(filepath.Join(home, ".claude/agents/reader.md")); e != nil {
					t.Fatal(e)
				}
				putProjectFile(t, home, ".claude/agents/README.md", "help")
			case "invalid name":
				putProjectFile(t, home, ".claude/agents/reader.md", "---\nname: ../../ESCAPED\ndescription: Reads.\n---\nRead.\n")
			}
			var out, errOut bytes.Buffer
			rc := dispatch([]string{"global"}, &out, &errOut, e)
			if name == "conflict" || name == "invalid name" {
				if rc != 1 {
					t.Errorf("unsafe global rc=%d: %s%s", rc, &out, &errOut)
				}
				return
			}
			if rc != 0 {
				t.Errorf("global rc=%d: %s%s", rc, &out, &errOut)
				return
			}
			out.Reset()
			errOut.Reset()
			rc = dispatch([]string{"doctor", "--machine"}, &out, &errOut, e)
			if rc != 0 {
				t.Errorf("doctor disagrees rc=%d: %s%s", rc, &out, &errOut)
			}
			if name != "empty source" {
				out.Reset()
				errOut.Reset()
				rc = dispatch([]string{"codex", "agents"}, &out, &errOut, e)
				if rc != 0 {
					t.Errorf("agents rc=%d: %s%s", rc, &out, &errOut)
				}
				if name == "policy homes" {
					if _, e := os.Stat(filepath.Join(home, ".codex", "agents", "reader.toml")); e == nil {
						t.Error("agents wrote wrong home")
					}
				}
			}
		})
	}
}

func TestReviewMachineNameCollisions(t *testing.T) {
	for _, names := range [][2]string{{"Reader", "reader"}, {"caf\u00e9", "cafe\u0301"}} {
		t.Run(names[0], func(t *testing.T) {
			home := testjail.Scratch(t)
			for i, name := range names {
				putProjectFile(t, home, fmt.Sprintf(".claude/agents/source%d.md", i), "---\nname: "+name+"\ndescription: Reads.\n---\nRead.\n")
			}
			e := env{get: func(k string) string {
				if k == "HOME" {
					return home
				}
				return ""
			}}
			var out, errOut bytes.Buffer
			rc := dispatch([]string{"global"}, &out, &errOut, e)
			if rc == 0 || !strings.Contains(out.String()+errOut.String(), "source0.md") || !strings.Contains(out.String()+errOut.String(), "source1.md") {
				t.Errorf("collision lost %d %s%s", rc, &out, &errOut)
			}
		})
	}
}
