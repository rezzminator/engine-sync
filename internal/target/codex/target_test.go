package codex

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func containsFinding(items []string, needle string) bool {
	for _, item := range items {
		if strings.Contains(strings.ToLower(item), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func writeCodexFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readCodexFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertCodexContains(t *testing.T, path string, needles ...string) {
	t.Helper()
	body := string(readCodexFixture(t, path))
	for _, needle := range needles {
		if !strings.Contains(body, needle) {
			t.Fatalf("%s missing %q:\n%s", path, needle, body)
		}
	}
}

func snapshotTestTree(t *testing.T, roots ...string) string {
	t.Helper()
	rows := []string{}
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}
				rows = append(rows, root+"/"+rel+" -> "+target)
				return nil
			}
			rows = append(rows, root+"/"+rel+" = "+string(readCodexFixture(t, path)))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

func TestLoadConfigRejectsUnknownFieldsAndBadVersion(t *testing.T) {
	root := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, ".claude", "codex-build.json"), `{"version":1,"mystery":true}`)
	if _, err := config.LoadConfigLayers(root); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("unknown field: got %v", err)
	}

	writeCodexFixture(t, filepath.Join(root, ".claude", "codex-build.json"), `{"version":2}`)
	if _, err := config.LoadConfigLayers(root); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("bad version: got %v", err)
	}
}

func TestGlobalCommandsCanBeDisabledWithoutTouchingGlobalOutputs(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
	writeCodexFixture(t, filepath.Join(root, ".claude", "commands", "local.md"), "---\ndescription: local\n---\nlocal\n")
	writeCodexFixture(t, filepath.Join(home, ".claude", "commands", "global.md"), "---\ndescription: global\n---\nglobal\n")

	// The default remains compatible: global outputs are produced when the
	// project does not opt out.
	if result, err := RunProject(Options{Root: root, Home: home, Mode: ModeBuild}); err != nil || !result.OK {
		t.Fatalf("seed build: result=%#v err=%v", result, err)
	}
	globalPrompt := filepath.Join(home, ".codex", "prompts", "global.md")
	globalSkill := filepath.Join(home, ".codex", "skills", "global", "SKILL.md")
	beforePrompt := string(readCodexFixture(t, globalPrompt))
	beforeSkill := string(readCodexFixture(t, globalSkill))

	writeCodexFixture(t, filepath.Join(root, ".claude", "codex-build.json"), `{"version":1,"globalCommands":false}`)
	writeCodexFixture(
		t,
		filepath.Join(home, ".claude", "commands", "global.md"),
		"---\ndescription: changed\n---\nchanged\n",
	)
	result, err := RunProject(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("opt-out build: result=%#v err=%v", result, err)
	}
	if got := string(readCodexFixture(t, globalPrompt)); got != beforePrompt {
		t.Fatalf("global prompt changed while disabled: %q", got)
	}
	if got := string(readCodexFixture(t, globalSkill)); got != beforeSkill {
		t.Fatalf("global skill changed while disabled: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".codex", "skills", "local", "SKILL.md")); err != nil {
		t.Fatalf("repository output missing while globals disabled: %v", err)
	}
	check, err := RunProject(Options{Root: root, Home: home, Mode: ModeCheck})
	if err != nil || !check.OK {
		t.Fatalf("disabled global source should not make check stale: result=%#v err=%v", check, err)
	}

	// Disabling global reconciliation must not weaken the independent TOML
	// safety gate: malformed existing global artifacts remain visible.
	writeCodexFixture(t, filepath.Join(home, ".codex", "config.toml"), "broken = [\n")
	check, err = RunProject(Options{Root: root, Home: home, Mode: ModeCheck})
	if err != nil || check.OK || !containsFinding(check.Problems, "UNPARSEABLE") {
		t.Fatalf("malformed global TOML was not reported: result=%#v err=%v", check, err)
	}
}

func TestGlobalCommandsConfigDefaultsTrue(t *testing.T) {
	root := testjail.Scratch(t)
	cfg, err := config.LoadConfigLayers(root)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Codex.GlobalCommands {
		t.Fatal("GlobalCommands default = false, want true")
	}
	writeCodexFixture(t, filepath.Join(root, ".claude", "codex-build.json"), `{"version":1,"globalCommands":false}`)
	cfg, err = config.LoadConfigLayers(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Codex.GlobalCommands {
		t.Fatal("GlobalCommands explicit false was ignored")
	}
}

// TestGlobalCommandUninstallDeletesTheOrphanedSkillDirectorySymlink is the
// regression for a dangling uninstall leftover: compileGlobalCommands
// mirrors a skill-directory global command (a {home}/.claude/commands/<name>
// directory carrying its own SKILL.md, e.g. "tokens") into
// {home}/.codex/skills/<name> as a bare SYMLINK into that source directory —
// the one output shape this compiler writes that cannot carry the marker
// artifact.Claims looks for. When the installer tears the command down
// (unwireGlobalRegistries in the installer package) and then reconciles the
// Codex mirror against an intentionally empty source, the old markerClaimable
// treated every symlink in the managed registry as foreign and left the now
// pointing-nowhere .codex/skills/<name> link behind — INSTALL.md § Uninstall
// promises every installer-owned link is removed. Ownership here is decided
// by the symlink's TARGET the same way global_unwire.go's ownedGlobalLink
// decides registry-link ownership: pointing inside {home}/.claude/commands
// names it as this compiler's, whatever else sits there afterward.
func TestGlobalCommandUninstallDeletesTheOrphanedSkillDirectorySymlink(t *testing.T) {
	home := testjail.Scratch(t)
	source := filepath.Join(home, ".claude", "commands", "tokens", "SKILL.md")
	writeCodexFixture(t, source, "---\ndescription: tokens\n---\nUse /tokens.\n")

	build, err := RunGlobalCommands(GlobalCommandsOptions{Home: home, Mode: ModeBuild})
	if err != nil || !build.OK {
		t.Fatalf("initial global build: result=%#v err=%v", build, err)
	}
	link := filepath.Join(home, ".codex", "skills", "tokens")
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink after build: info=%#v err=%v", link, info, err)
	}

	// Simulate the uninstall order: the source command directory is gone
	// (unwireGlobalRegistries already retired it) before the Codex mirror is
	// reconciled against an intentionally empty SourceHome, the exact shape
	// reconcileCodexCommands(nil) in installer.go uses for ModeUninstall.
	if err := os.RemoveAll(filepath.Join(home, ".claude", "commands", "tokens")); err != nil {
		t.Fatal(err)
	}
	emptySource := testjail.Scratch(t)
	uninstallBuild, err := RunGlobalCommands(
		GlobalCommandsOptions{Home: home, SourceHome: emptySource, Mode: ModeBuild},
	)
	if err != nil || !uninstallBuild.OK {
		t.Fatalf("uninstall reconciliation: result=%#v err=%v", uninstallBuild, err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("uninstall left the dangling Codex skill symlink %s behind: %v", link, err)
	}
}

func TestMCPBackedChatCommandsRetireGlobalSkillsButKeepInterrogate(t *testing.T) {
	home := testjail.Scratch(t)
	for relative := range map[string]bool{
		"chat/inject.md":      true,
		"chat/interrogate.md": true,
		"reload.md":           true,
	} {
		writeCodexFixture(
			t,
			filepath.Join(home, ".claude", "commands", relative),
			"---\ndescription: fixture\n---\nfixture\n",
		)
	}
	managedInject := filepath.Join(home, ".codex", "skills", "chat-inject", "SKILL.md")
	writeCodexFixture(t, managedInject, generatedHeader("old-chat-inject")+"\n")
	policy := config.Default()
	policy.Machine.RetiredFiles = []string{".codex/skills/chat-inject"}
	result, err := RunGlobalCommands(GlobalCommandsOptions{Home: home, Mode: ModeBuild, Config: &policy})
	if err != nil || !result.OK {
		t.Fatalf("global command reconciliation: result=%#v err=%v", result, err)
	}
	for _, path := range []string{
		filepath.Join(home, ".codex", "prompts", "chat-inject.md"),
		filepath.Join(home, ".codex", "prompts", "chat-interrogate.md"),
		filepath.Join(home, ".codex", "skills", "chat-interrogate", "SKILL.md"),
		filepath.Join(home, ".codex", "skills", "reload", "SKILL.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected global artifact %s: %v", path, err)
		}
	}
	if _, err := os.Stat(managedInject); !os.IsNotExist(err) {
		t.Fatalf("MCP-backed global chat skill remains: %v", err)
	}
}

// TestCommandSwapNeverRewritesASlashContinuedPath is the regression for a
// real corruption: swapCommands matched a command name by its PREFIX only,
// so a filesystem path that merely starts with a command name (/dev/tty)
// was rewritten right along with the actual command invocation (/dev build
// app). A slash command is followed by whitespace, end-of-line, or
// punctuation — never by another "/".
func TestCommandSwapNeverRewritesASlashContinuedPath(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, ".claude", "commands", "dev.md"), "---\ndescription: dev\n---\ndev\n")
	writeCodexFixture(
		t,
		filepath.Join(root, "CLAUDE.md"),
		"# Fixture\n\nTUI renders on /dev/tty. Use `/dev build app` for anything the pipeline reads.\n",
	)
	result, err := RunProject(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("build: result=%#v err=%v", result, err)
	}
	agents := string(readCodexFixture(t, filepath.Join(root, "AGENTS.md")))
	if !strings.Contains(agents, "TUI renders on /dev/tty.") {
		t.Fatalf("a /-continued path was rewritten, corrupting it:\n%s", agents)
	}
	if !strings.Contains(agents, "Use `$dev build app` for anything the pipeline reads.") {
		t.Fatalf("the real /dev command invocation was not rewritten:\n%s", agents)
	}
}

// A line naming both files states their relation; swapping its CLAUDE.md
// makes it claim AGENTS.md is hand-edited and compiled from itself.
func TestTransformMarkdownKeepsLinesNamingBothFiles(t *testing.T) {
	both := "- `CLAUDE.md` is the one hand-edited orientation file; `AGENTS.md` is compiled from it."
	pointer := "- app: the main program · child `app/CLAUDE.md`"
	got := transformMarkdown(both+"\n"+pointer+"\n", TransformOptions{ReplaceClaudeFile: true})
	want := both + "\n- app: the main program · child `app/AGENTS.md`\n"
	if got != want {
		t.Fatalf("transformMarkdown = %q, want %q", got, want)
	}
}

func TestFrontmatterUnquotesYAMLScalars(t *testing.T) {
	raw := "---\n" +
		"single: 'a: b, \"c\" and it''s fine'\n" +
		"double: \"line one\\n\\\"line two\\\" \\\\ end\"\n" +
		"---\nbody\n"
	front, _, err := claudesource.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	fields := front.Fields
	want := map[string]string{
		"single": `a: b, "c" and it's fine`,
		"double": "line one\n\"line two\" \\ end",
	}
	for key, expected := range want {
		if got := fields[key]; got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestFrontmatterStripsPlainScalarCommentAndPreservesQuotedHash(t *testing.T) {
	raw := "---\nmodel: opus # pinned\nquoted: 'keep # this'\n---\nbody\n"
	front, _, err := claudesource.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	fields := front.Fields
	if got := fields["model"]; got != "opus" {
		t.Fatalf("model = %q, want %q", got, "opus")
	}
	if got := fields["quoted"]; got != "keep # this" {
		t.Fatalf("quoted = %q, want %q", got, "keep # this")
	}
	// A top-level line strict YAML refuses is read as its literal text (the
	// compatibility retry); a nested one still fails, naming its line.
	if front, _, err := claudesource.Parse("---\nbad: \"invalid\\x\"\n---\nbody\n"); err != nil || front.Fields["bad"] != `"invalid\x"` {
		t.Fatalf("top-level malformed quoted scalar = %q, %v", front.Fields["bad"], err)
	}
	if _, _, err := claudesource.Parse("---\nok: 1\nnested:\n  bad: \"invalid\\x\"\n---\nbody\n"); err == nil ||
		!strings.Contains(err.Error(), "line 4") {
		t.Fatalf("nested malformed quoted scalar error = %v", err)
	}
}

func TestDanglingGlobalCommandIsHonestAndCheckWritesNothing(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
	commandDir := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-command.md", filepath.Join(commandDir, "gone.md")); err != nil {
		t.Fatal(err)
	}

	build, err := RunProject(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !containsFinding(build.Warnings, "dangling") {
		t.Fatalf("build warnings = %#v", build.Warnings)
	}

	if err := os.Remove(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	check, err := RunProject(Options{Root: root, Home: home, Mode: ModeCheck})
	if err != nil {
		t.Fatalf("check execution: %v", err)
	}
	if check.OK || !containsFinding(check.Problems, "dangling") || !containsFinding(check.Problems, "MISSING") {
		t.Fatalf("check = %#v", check)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("check wrote AGENTS.md: %v", err)
	}
}

func TestIncumbentUnionFixtureBuildThenReadOnlyCheck(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, ".claude", "codex-build.json"), `{
  "version": 1,
  "modelMap": {"sonnet":"gpt-fixture","opus":"gpt-5.6-sol"},
  "rootAdapter": "\n## Fixture adapter\n",
  "agentPreamble": "Role ${name} starts here.\n\n",
  "excludeDirs": ["references"],
  "excludeProjects": ["template"],
  "neverRegister": ["private"],
  "suffixMode": "strip-prefix",
  "suffixPrefix": "sample-"
}`)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Root\n\nUse /tools:go with sonnet. Read CLAUDE.md.\n")
	writeCodexFixture(t, filepath.Join(root, "sample-api", "CLAUDE.md"), "# API\n")
	writeCodexFixture(t, filepath.Join(root, "template", "CLAUDE.md"), "# Must stay excluded\n")
	writeCodexFixture(
		t,
		filepath.Join(root, ".claude", "commands", "tools", "go.md"),
		"---\ndescription: >-\n  Run sonnet cards\n  safely\n---\nBody keeps CLAUDE.md and /tools:go with sonnet.\n",
	)
	writeCodexFixture(t, filepath.Join(root, ".claude", "commands", "references", "killed.md"), "killed\n")
	writeCodexFixture(
		t,
		filepath.Join(root, ".claude", "agents", "reviewer.md"),
		"---\ndescription: >-\n  Review \\\"quoted\\\" output\nmodel: opus # pinned\neffort: high\ntools: Read, Grep\n---\nFollow /tools:go.\n",
	)
	writeCodexFixture(
		t,
		filepath.Join(root, ".claude", "agents", "unmapped.md"),
		"---\ndescription: unmapped model\nengines:\n  codex:\n    model: something-else\n---\nunmapped\n",
	)
	writeCodexFixture(
		t,
		filepath.Join(root, ".claude", "agents", "escaped.md"),
		"---\ndescription: escaped values\nengines:\n  codex:\n    model: 'model\\path\"quoted'\neffort: high\n---\nescaped\n",
	)
	writeCodexFixture(t, filepath.Join(root, ".claude", "agents", "private.md"), "---\ndescription: private\n---\nno\n")
	writeCodexFixture(
		t,
		filepath.Join(root, ".claude", "agents", "changelogger.md"),
		"---\ndescription: writer\ntools: Read, Write, Edit, Bash, Glob, Grep, Agent\n---\nwriter\n",
	)
	writeCodexFixture(
		t,
		filepath.Join(root, ".claude", "agents", "gitter.md"),
		"---\ndescription: git writer\ntools: Read, Bash, Glob, Grep\nengines:\n  codex:\n    sandbox: inherit\n---\ngitter\n",
	)
	writeCodexFixture(
		t,
		filepath.Join(root, "sample-api", ".claude", "agents", "gitter.md"),
		"---\ndescription: nested git writer\ntools: Read, Bash, Glob, Grep\nengines:\n  codex:\n    sandbox: inherit\n---\ngitter\n",
	)
	writeCodexFixture(
		t,
		filepath.Join(root, "sample-api", ".claude", "agents", "worker.md"),
		"---\ndescription: child\n---\nchild\n",
	)
	writeCodexFixture(t, filepath.Join(root, ".claude", "skills", "native", "SKILL.md"), "# Native\n")
	writeCodexFixture(t, filepath.Join(home, ".claude", "commands", "global.md"), "---\ndescription: global\n---\nglobal\n")
	writeCodexFixture(
		t,
		filepath.Join(home, ".claude", "commands", "side.md"),
		"---\ndescription: side\ndisable-model-invocation: true\n---\nside\n",
	)
	writeCodexFixture(t, filepath.Join(root, ".codex", "config.toml"), "model = \"fixture\"\n")
	writeCodexFixture(
		t,
		filepath.Join(root, ".mcp.json"),
		`{"mcpServers":{"fixture":{"command":"app","args":["mcp"],"future":true}}}`,
	)

	build, err := RunProject(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !build.OK {
		t.Fatalf("build: result=%#v err=%v", build, err)
	}
	if !containsFinding(build.Warnings, "fields not mapped") {
		t.Fatalf("MCP warning missing: %#v", build.Warnings)
	}
	assertCodexContains(
		t,
		filepath.Join(root, "AGENTS.md"),
		"Generated by engine-sync from",
		"$tools-go",
		"gpt-fixture",
		"AGENTS.md",
		"## Fixture adapter",
	)
	if _, err := os.Stat(filepath.Join(root, "template", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("excluded template AGENTS.md exists: %v", err)
	}
	assertCodexContains(
		t,
		filepath.Join(root, ".codex", "agents", "reviewer.toml"),
		`description = "Review \\\"quoted\\\" output"`,
		`model = "gpt-5.6-sol"`,
		`model_reasoning_effort = "high"`,
		`sandbox_mode = "read-only"`,
		"Role reviewer starts here.",
		"$tools-go",
	)
	reviewer := string(readCodexFixture(t, filepath.Join(root, ".codex", "agents", "reviewer.toml")))
	ordered := []string{
		`name = "reviewer"`,
		`description = "Review \\\"quoted\\\" output"`,
		`model = "gpt-5.6-sol"`,
		`model_reasoning_effort = "high"`,
		`sandbox_mode = "read-only"`,
		`developer_instructions = """`,
	}
	last := -1
	for _, needle := range ordered {
		index := strings.Index(reviewer, needle)
		if index <= last {
			t.Fatalf("reviewer.toml key %q at %d after prior key at %d:\n%s", needle, index, last, reviewer)
		}
		last = index
	}
	assertCodexContains(t, filepath.Join(root, ".codex", "agents", "unmapped.toml"), `model = "something-else"`)
	assertCodexContains(
		t,
		filepath.Join(root, ".codex", "agents", "escaped.toml"),
		`model = "model\\path\"quoted"`,
		`model_reasoning_effort = "high"`,
	)
	worker := string(readCodexFixture(t, filepath.Join(root, ".codex", "agents", "worker-api.toml")))
	if !strings.Contains(worker, `name = "worker-api"`) || strings.Contains(worker, "\nmodel =") ||
		strings.Contains(worker, "\nmodel_reasoning_effort =") {
		t.Fatalf("worker-api.toml must omit absent model and effort keys:\n%s", worker)
	}
	for _, writable := range []string{"changelogger", "gitter", "gitter-api", "unmapped"} {
		content := string(readCodexFixture(t, filepath.Join(root, ".codex", "agents", writable+".toml")))
		if strings.Contains(content, "sandbox_mode") {
			t.Fatalf("%s.toml must carry no sandbox_mode key:\n%s", writable, content)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".codex", "agents", "private.toml")); !os.IsNotExist(err) {
		t.Fatalf("never-register agent exists: %v", err)
	}
	commandSkill := filepath.Join(root, ".codex", "skills", "tools-go", "SKILL.md")
	assertCodexContains(
		t,
		commandSkill,
		"name: tools-go",
		"Run sonnet cards",
		"safely",
		"Body keeps CLAUDE.md",
		"$tools-go",
		"gpt-fixture",
	)
	if strings.Contains(string(readCodexFixture(t, commandSkill)), "Run gpt-fixture cards") {
		t.Fatal("command frontmatter model alias was rewritten")
	}
	if _, err := os.Stat(
		filepath.Join(root, ".codex", "skills", "references-killed", "SKILL.md"),
	); !os.IsNotExist(
		err,
	) {
		t.Fatalf("excluded command exists: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(root, ".codex", "skills", "native")); err != nil || target == "" {
		t.Fatalf("native skill link: target=%q err=%v", target, err)
	}
	assertCodexContains(t, filepath.Join(home, ".codex", "prompts", "side.md"), "name: side")
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "side", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("non-model-invocable global skill exists: %v", err)
	}
	assertCodexContains(
		t,
		filepath.Join(root, ".codex", "config.toml"),
		`model = "fixture"`,
		artifact.MCPBegin,
		"[mcp_servers.fixture]",
	)

	before := snapshotTestTree(t, root, home)
	check, err := RunProject(Options{Root: root, Home: home, Mode: ModeCheck})
	if err != nil || !check.OK {
		t.Fatalf("check: result=%#v err=%v", check, err)
	}
	if after := snapshotTestTree(t, root, home); after != before {
		t.Fatalf("check wrote files:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestCheckRejectsInvalidKeeperTOML(t *testing.T) {
	for _, surface := range []string{"project config", "home config", "project agents", "home agents"} {
		t.Run(surface, func(t *testing.T) {
			root, home := testjail.Scratch(t), testjail.Scratch(t)
			writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
			writeCodexFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"codex":{"globalCommands":false}}`)
			base := root
			if strings.HasPrefix(surface, "home") {
				base = home
			}
			path := filepath.Join(base, ".codex", "config.toml")
			if strings.HasSuffix(surface, "agents") {
				path = filepath.Join(base, ".codex", "agents", "keeper.toml")
			}
			writeCodexFixture(t, path, "broken = [\n")
			result, err := RunProject(Options{Root: root, Home: home, Mode: ModeCheck})
			if err != nil || result.OK || !containsFinding(result.Problems, "UNPARSEABLE "+path) {
				t.Fatalf("invalid TOML check = %#v, err=%v", result, err)
			}
		})
	}
}

func TestReconcileFindingsAreNamedAndNonDestructive(t *testing.T) {
	t.Run("stale and orphan check findings", func(t *testing.T) {
		root := testjail.Scratch(t)
		home := testjail.Scratch(t)
		writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")

		if result, err := RunProject(Options{Root: root, Home: home, Mode: ModeBuild}); err != nil || !result.OK {
			t.Fatalf("seed build: result=%#v err=%v", result, err)
		}
		writeCodexFixture(t, filepath.Join(root, "AGENTS.md"), artifact.Marker(artifact.Markdown, "CLAUDE.md")+"\nstale generated content\n")
		writeCodexFixture(
			t,
			filepath.Join(root, ".codex", "agents", "orphan.toml"),
			artifact.Marker(artifact.TOML, "old.md")+"\nname = \"orphan\"\n",
		)
		before := snapshotTestTree(t, root, home)

		result, err := RunProject(Options{Root: root, Home: home, Mode: ModeCheck})
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if result.OK || !containsFinding(result.Problems, "STALE") || !containsFinding(result.Problems, "ORPHAN") {
			t.Fatalf("check findings = %#v", result)
		}
		if after := snapshotTestTree(t, root, home); after != before {
			t.Fatalf("check changed files:\nbefore=%s\nafter=%s", before, after)
		}
	})

	t.Run("build conflict is named and preserved", func(t *testing.T) {
		root := testjail.Scratch(t)
		home := testjail.Scratch(t)
		writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
		writeCodexFixture(
			t,
			filepath.Join(root, ".claude", "agents", "reviewer.md"),
			"---\ndescription: reviewer\n---\nReview.\n",
		)
		if result, err := RunProject(Options{Root: root, Home: home, Mode: ModeBuild}); err != nil || !result.OK {
			t.Fatalf("seed build: result=%#v err=%v", result, err)
		}

		conflictPath := filepath.Join(root, ".codex", "agents", "reviewer.toml")
		writeCodexFixture(t, conflictPath, "hand-written keeper\n")
		before := snapshotTestTree(t, root, home)
		result, err := RunProject(Options{Root: root, Home: home, Mode: ModeBuild})
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if result.OK || !containsFinding(result.Problems, "CONFLICT") {
			t.Fatalf("build findings = %#v", result)
		}
		if after := snapshotTestTree(t, root, home); after != before {
			t.Fatalf("conflict build changed files:\nbefore=%s\nafter=%s", before, after)
		}
		if got := string(readCodexFixture(t, conflictPath)); got != "hand-written keeper\n" {
			t.Fatalf("conflict file changed: %q", got)
		}
	})
}

func TestWorkbenchRootIgnoresGlobalCommands(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "Scribe.\n")
	writeCodexFixture(t, filepath.Join(root, ".example", "workbench.json"), `{}`)
	writeCodexFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"codex":{"globalCommands":true},"workbenchMarker":".example/workbench.json"}`)
	writeCodexFixture(t, filepath.Join(root, ".claude", "agents", "clerk.md"), "---\ndescription: Clerk.\n---\nClerk.\n")
	writeCodexFixture(t, filepath.Join(home, ".claude", "commands", "memo.md"), "---\ndescription: Memo.\n---\nMemo.\n")
	sentinel := filepath.Join(home, ".codex", "prompts", "kept.md")
	body := generatedHeader("fixture") + "\nKept.\n"
	writeCodexFixture(t, sentinel, body)
	result, err := Build(Options{Root: root, Home: home})
	if err != nil || !result.OK {
		t.Fatalf("build=%#v err=%v", result, err)
	}
	for _, path := range []string{filepath.Join(root, "AGENTS.md"), filepath.Join(root, ".codex", "agents", "clerk.toml")} {
		if _, err := os.Stat(path); err != nil {
			t.Error(err)
		}
	}
	for _, path := range []string{filepath.Join(home, ".codex", "prompts", "memo.md"), filepath.Join(home, ".codex", "skills", "memo")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("home output %s: %v", path, err)
		}
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != body {
		t.Errorf("home sentinel=%q err=%v", data, err)
	}
}
