package cli

import (
	"bytes"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalUsesClaudeConfigSourcesAndPreservesThem(t *testing.T) {
	home, configDir := testjail.Scratch(t), testjail.Scratch(t)
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	source := "---\nname: writer\ndescription: Writes git.\ntools: Read, Bash\nengines:\n  codex:\n    sandbox: inherit\n---\nchildren are Explore+haiku (never\nyour own type)\n"
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
				putProjectFile(t, home, ".claude/engine-sync.json", `{"version":1,"machine":{"codexHomes":["`+chosen+`"]}}`)
			case "targets homes":
				putTargets(t, home, `{"codexHomes":["`+chosen+`"]}`)
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

// installManaged runs `global` over one agent and one command with the given
// global file and returns the home it wrote and the clone it read.
func installManaged(t *testing.T, policy string) (home, repo string) {
	t.Helper()
	home, _, e := auditEnv(t)
	repo = filepath.Join(home, "clone")
	putProjectFile(t, repo, "CLAUDE.md", "Clone\n")
	putProjectFile(t, repo, "agents/astra.md", "---\nname: astra\ndescription: Astra.\ntools: Read\n---\nAstra.\n")
	putProjectFile(t, home, ".claude/commands/hello.md", "---\ndescription: Hello.\n---\nHello\n")
	putProjectFile(t, home, ".claude/engine-sync.json", policy)
	auditOK(t, e, "global", "--source-repo", repo)
	return home, repo
}

func fileLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(raw), "\n")
}

// machine.managedBy names the installer on the line after the generated
// header of the four regular-file kinds `global` writes into a machine
// registry; unset, every byte is as it was, and a project build never
// carries the line.
func TestGlobalWritesTheManagedByLine(t *testing.T) {
	const line = "# managed-by: app"
	home, repo := installManaged(t, `{"version":1,"machine":{"sourceGeneratedAgentsDir":"generated/agents","managedBy":"app"}}`)
	role := fileLines(t, filepath.Join(home, ".codex", "agents", "astra.toml"))
	if !strings.HasPrefix(role[0], "# Generated by engine-sync") || role[1] != line {
		t.Errorf("Codex role lines 1-2 = %q, want the header then %q", role[:2], line)
	}
	agent := fileLines(t, filepath.Join(home, ".config", "opencode", "agents", "astra.md"))
	if agent[0] != "---" || !strings.HasPrefix(agent[1], "# Generated by engine-sync") || agent[2] != line {
		t.Errorf("OpenCode agent lines 1-3 = %q, want ---, the header, then %q", agent[:3], line)
	}
	prompt := fileLines(t, filepath.Join(home, ".codex", "prompts", "hello.md"))
	if prompt[0] != "---" || !strings.HasPrefix(prompt[1], "# Generated by engine-sync") || prompt[2] != line {
		t.Errorf("Codex prompt lines 1-3 = %q, want ---, the header, then %q", prompt[:3], line)
	}
	command := fileLines(t, filepath.Join(home, ".config", "opencode", "command", "hello.md"))
	if command[0] != "---" || !strings.HasPrefix(command[1], "# Generated by engine-sync") || command[2] != line {
		t.Errorf("OpenCode command lines 1-3 = %q, want ---, the header, then %q", command[:3], line)
	}
	for _, path := range []string{filepath.Join(home, ".codex", "skills", "hello", "SKILL.md"), filepath.Join(repo, "generated")} {
		if raw, err := os.ReadFile(path); err == nil && strings.Contains(string(raw), "managed-by") {
			t.Errorf("%s carries the managed-by line: %s", path, raw)
		}
	}
	e := env{get: func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}}
	if rc, out := auditRun(e, "doctor", "--machine", "--source-repo", repo); rc != 0 || strings.Contains(out, "STALE") {
		t.Errorf("doctor after a marked install rc=%d, want 0 with no STALE row: %s", rc, out)
	}

	plain, _ := installManaged(t, `{"version":1,"machine":{"sourceGeneratedAgentsDir":"generated/agents"}}`)
	for _, rel := range []string{".codex/agents/astra.toml", ".config/opencode/agents/astra.md", ".codex/prompts/hello.md", ".config/opencode/command/hello.md"} {
		marked := fileLines(t, filepath.Join(home, filepath.FromSlash(rel)))
		unmarked := fileLines(t, filepath.Join(plain, filepath.FromSlash(rel)))
		var without []string
		for _, l := range marked {
			if l != line {
				without = append(without, l)
			}
		}
		if strings.Join(without, "\n") != strings.Join(unmarked, "\n") {
			t.Errorf("%s: dropping the managed-by line leaves %q, want today's bytes %q", rel, without, unmarked)
		}
		if len(without) != len(marked)-1 {
			t.Errorf("%s: want exactly one managed-by line", rel)
		}
	}
}

// A project build reads the global file's machine layer too, yet writes the
// managed-by line into no project mirror.
func TestProjectBuildNeverCarriesTheManagedByLine(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
	putProjectFile(t, root, ".claude/agents/reader.md", auditAgent)
	putProjectFile(t, root, ".claude/commands/hello.md", "---\ndescription: Hello.\n---\nHello\n")
	putProjectFile(t, home, ".claude/engine-sync.json", `{"version":1,"machine":{"managedBy":"app"}}`)
	e := env{get: func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}}
	auditOK(t, e, "build", root)
	var seen int
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return err
		}
		seen++
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(raw), "managed-by") {
			t.Errorf("project file %s carries the managed-by line", path)
		}
		return nil
	})
	if err != nil || seen < 4 {
		t.Fatalf("walked %d project files: %v", seen, err)
	}
}

// A file engine-sync wrote that its marker names and no source produces any
// more is a failing ORPHAN row ending in the installer it names: a Codex
// role, an OpenCode agent, a Codex prompt and an OpenCode command alike. The
// same file without the managed-by line keeps today's row.
func TestDoctorNamesAMarkedOrphan(t *testing.T) {
	const suffix = " (managed-by app)"
	home, repo := installManaged(t, `{"version":1,"machine":{"sourceGeneratedAgentsDir":"generated/agents","managedBy":"app"}}`)
	marked := func(syntax artifact.Syntax, source string) string {
		return artifact.Marker(syntax, source) + "\n" + artifact.ManagedByLine(syntax, "app") + "\n"
	}
	global := func(syntax artifact.Syntax, source string) string {
		return artifact.GlobalMarker(syntax, source) + "\n"
	}
	planted := map[string]string{
		".codex/agents/old-role.toml":             marked(artifact.TOML, "agents/old-role.md") + "name = \"old-role\"\n",
		".config/opencode/agents/old-agent.md":    "---\n" + marked(artifact.YAMLFront, "agents/old-agent.md") + "description: \"Old.\"\n---\nOld.\n",
		".codex/prompts/old-prompt.md":            "---\n" + marked(artifact.YAMLFront, "$HOME/.claude/commands/old-prompt.md") + "name: old-prompt\n---\nOld.\n",
		".config/opencode/command/old-command.md": "---\n" + marked(artifact.YAMLFront, "$HOME/.claude/commands/old-command.md") + "description: \"Old.\"\n---\nOld.\n",
		".codex/agents/plain-role.toml":           global(artifact.TOML, "agents/plain-role.md") + "name = \"plain-role\"\n",
		".codex/prompts/plain-prompt.md":          "---\n" + artifact.Marker(artifact.YAMLFront, "$HOME/.claude/commands/plain-prompt.md") + "\nname: plain-prompt\n---\nOld.\n",
	}
	for rel, content := range planted {
		putProjectFile(t, home, rel, content)
	}
	e := env{get: func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}}
	rc, out := auditRun(e, "doctor", "--machine", "--source-repo", repo)
	if rc != 1 {
		t.Fatalf("doctor rc=%d, want 1 (the exit code a failing row gives): %s", rc, out)
	}
	rows := strings.Split(strings.TrimSpace(out), "\n")
	rowFor := func(rel string) string {
		path := filepath.Join(home, filepath.FromSlash(rel))
		for _, row := range rows {
			if strings.Contains(row, path) {
				return row
			}
		}
		return ""
	}
	for _, rel := range []string{".codex/agents/old-role.toml", ".config/opencode/agents/old-agent.md", ".codex/prompts/old-prompt.md", ".config/opencode/command/old-command.md"} {
		row := rowFor(rel)
		if !strings.HasPrefix(row, "fail agent ORPHAN ") || !strings.HasSuffix(row, suffix) {
			t.Errorf("%s: row %q, want a failing `agent ORPHAN … %s` row", rel, row, suffix)
		}
		for _, other := range rows {
			if other != row && strings.Contains(other, filepath.Join(home, filepath.FromSlash(rel))) {
				t.Errorf("%s: a second row names it: %q", rel, other)
			}
		}
	}
	if row := rowFor(".codex/agents/plain-role.toml"); !strings.Contains(row, "agent STALE codex plain-role") || strings.Contains(row, "ORPHAN") {
		t.Errorf("unmarked role: row %q, want today's STALE row", row)
	}
	if row := rowFor(".codex/prompts/plain-prompt.md"); !strings.HasPrefix(row, "fail delete ") || strings.Contains(row, "ORPHAN") {
		t.Errorf("unmarked prompt: row %q, want today's `fail delete` row", row)
	}
}
