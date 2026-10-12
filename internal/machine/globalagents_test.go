package machine

import (
	"github.com/BurntSushi/toml"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertGlobalSymlink fails the test unless target is a symlink resolving
// exactly to source — the shape every global registry install now promises
// in place of the old copy.
func assertGlobalSymlink(t *testing.T, target, source string) {
	t.Helper()
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("expected install at %s: %v", target, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink (mode=%v)", target, info.Mode())
	}
	resolved, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("readlink %s: %v", target, err)
	}
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(target), resolved)
	}
	if filepath.Clean(resolved) != filepath.Clean(source) {
		t.Fatalf("%s -> %s, want -> %s", target, resolved, source)
	}
}

// assertGlobalRoleFile fails the test unless target is a regular file carrying
// pfm's generated marker — the shape Codex loads and the proof pfm owns it.
func assertGlobalRoleFile(t *testing.T, target string) {
	t.Helper()
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("expected a role file at %s: %v", target, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s mode=%v — Codex refuses anything but a regular file", target, info.Mode())
	}
	if !GeneratedGlobalRole(readAgentFixture(t, target)) {
		t.Fatalf("%s carries no generated marker", target)
	}
}

// hasGlobalAgentLinkAction reports whether the plan carries the exact link
// action for one desired target — an action list that merely has the right
// LENGTH would pass while pointing everywhere but the account that is missing
// its agents.
func hasGlobalAgentLinkAction(actions []GlobalAgentAction, path, target string) bool {
	for _, action := range actions {
		if action.Kind == "link" && action.Path == path && action.Target == target {
			return true
		}
	}
	return false
}

func TestGlobalAgentCodexFrontmatterOverridesModelAndEffort(t *testing.T) {
	agentsDir := testjail.Scratch(t)
	path := filepath.Join(agentsDir, "worker.md")
	raw := "---\nname: worker\ndescription: Worker.\nmodel: sonnet\neffort: medium\nengines:\n  codex:\n    model: gpt-6-sol\n    effort: high\n---\n\nWork.\n"
	_, got, err := renderFixtureMachineAgent(path, raw, agentsDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "model = \"gpt-6-sol\"\nmodel_reasoning_effort = \"high\"\n") {
		t.Fatalf("Codex overrides missing from TOML:\n%s", got)
	}
	if strings.Contains(got, "engines") || strings.Contains(got, "gpt-6-sol\n    effort") {
		t.Fatalf("Codex-only frontmatter leaked into TOML:\n%s", got)
	}
}

func TestGlobalAgentBadCodexEffortNamesSource(t *testing.T) {
	agentsDir := testjail.Scratch(t)
	path := filepath.Join(agentsDir, "worker.md")
	for _, effort := range []string{"", "ultra", "HIGH"} {
		t.Run(effort, func(t *testing.T) {
			raw := "---\nname: worker\ndescription: Worker.\nengines:\n  codex:\n    effort: " + effort + "\n---\n\nWork.\n"
			_, _, err := renderFixtureMachineAgent(path, raw, agentsDir)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "engines.codex.effort") {
				t.Fatalf("error = %v, want source path and engines.codex.effort", err)
			}
		})
	}
}

// TestGlobalAgentsCompilesInstallsAndAppliesSpawnAgentSubstitution covers the
// two-file happy path: both .md sources compile to a sibling .toml, both get
// installed into {home}/.claude/agents (raw source) and {home}/.codex/agents
// (compiled TOML), and the one Codex-specific body substitution — "Codex has
// no Agent tool" — fires exactly where the host script's docstring says it
// does.
func TestGlobalAgentsCompilesInstallsAndAppliesSpawnAgentSubstitution(t *testing.T) {
	home := testjail.Scratch(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	writeAgentFixture(t, filepath.Join(home, ".config", "engine-sync", "config.json"), `{"version":1,"machine":{"rewrites":[{"engines":["codex"],"kinds":["agent"],"literal":"children are Explore+haiku (never\nyour own type)","replace":"children are spawned via spawn_agent as the `+"`explorer`"+` role (never your own type)"}]}}`)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\ntools: Read\nmodel: sonnet\n---\n\n"+
			"Delegate to children are Explore+haiku (never\nyour own type) for search fan-out.\n")
	writeAgentFixture(
		t,
		filepath.Join(home, "source-repo", "agents", "beta.md"),
		"---\nname: beta\ndescription: Beta role for testing.\ntools: Read\nmodel: haiku\n---\n\nBeta body, unrelated.\n",
	)

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	if len(result.Compiled) != 2 {
		t.Fatalf("compiled = %#v, want 2 entries", result.Compiled)
	}
	if len(result.Installed) != 2 {
		t.Fatalf("installed = %#v, want 2 Claude link entries", result.Installed)
	}
	for _, installed := range result.Installed {
		if installed.State != GlobalLinkMissing {
			t.Fatalf("installed %s classified %s before install ran, want missing", installed.Path, installed.State)
		}
	}
	if len(result.Roles) != 2 {
		t.Fatalf("roles = %#v, want 2 Codex role files", result.Roles)
	}
	for _, role := range result.Roles {
		if role.State != GlobalRoleMissing {
			t.Fatalf("role %s classified %s before install ran, want missing", role.Path, role.State)
		}
	}
	if len(result.Problems) != 0 {
		t.Fatalf("problems = %#v, want none for a fresh install", result.Problems)
	}

	alphaTOML := string(
		readAgentFixture(t, filepath.Join(filepath.Join(home, ".codex", "agents"), "alpha.toml")),
	)
	if strings.Contains(alphaTOML, "children are Explore+haiku") {
		t.Fatalf("alpha.toml: substitution did not fire:\n%s", alphaTOML)
	}
	if !strings.Contains(alphaTOML, "spawned via spawn_agent as the `explorer` role") {
		t.Fatalf("alpha.toml: substitution target text missing:\n%s", alphaTOML)
	}

	for _, expect := range []struct{ target, source string }{
		{filepath.Join(home, ".claude", "agents", "alpha.md"), filepath.Join(home, "source-repo", "agents", "alpha.md")},
		{filepath.Join(home, ".claude", "agents", "beta.md"), filepath.Join(home, "source-repo", "agents", "beta.md")},
	} {
		assertGlobalSymlink(t, expect.target, expect.source)
	}
	for _, name := range []string{"alpha.toml", "beta.toml"} {
		assertGlobalRoleFile(t, filepath.Join(home, ".codex", "agents", name))
	}

	// No .toml is ever written beside the .md source inside the clone —
	// the whole point of this move.
	for _, name := range []string{"alpha.toml", "beta.toml"} {
		inClone := filepath.Join(home, "source-repo", "agents", name)
		if _, err := os.Lstat(inClone); !os.IsNotExist(err) {
			t.Fatalf("expected no .toml written inside the clone at %s, lstat err=%v", inClone, err)
		}
	}

	// The .claude install is the raw source, untouched by the Codex-only
	// substitution — Claude does have an Agent tool.
	claudeAlpha := string(readAgentFixture(t, filepath.Join(home, ".claude", "agents", "alpha.md")))
	if !strings.Contains(claudeAlpha, "children are Explore+haiku (never\nyour own type)") {
		t.Fatalf(".claude/agents/alpha.md: raw source was mutated:\n%s", claudeAlpha)
	}
}

// TestGlobalAgentsAdversarialFixtureEmitsValidTOMLWithLiteralQuotesAndDelimiterCollision
// is the exact failure mode the host script's docstring warns about: a
// description containing a raw `"` (real examples: the trigger phrases
// "walker fast", "map it now") and a body containing a literal `"""` that
// collides with the multi-line basic-string delimiter. An emitter that drops
// either escape rule ships a TOML Codex cannot parse at startup — this test
// asserts both the exact escaped bytes AND that the result independently
// parses as TOML (via BurntSushi/toml, not our own escaping logic).
func TestGlobalAgentsAdversarialFixtureEmitsValidTOMLWithLiteralQuotesAndDelimiterCollision(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(
		t,
		filepath.Join(home, "source-repo", "agents", "quirky.md"),
		"---\nname: quirky\ndescription: Uses \"walker fast\" and \"map it now\" verbatim.\ntools: Read\nmodel: opus # pinned\neffort: high\n---\n\n"+
			"Body has a literal triple quote \"\"\" and a backslash \\ standalone.\n",
	)

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	if len(result.Compiled) != 1 {
		t.Fatalf("compiled = %#v, want 1 entry", result.Compiled)
	}

	got := string(
		readAgentFixture(t, filepath.Join(filepath.Join(home, ".codex", "agents"), "quirky.toml")),
	)
	head := ("# " + artifact.Prefix) + "agents/quirky.md" +
		" — edit the source, then run: engine-sync build\n" +
		"name = \"quirky\"\n" +
		"description = \"Uses \\\"walker fast\\\" and \\\"map it now\\\" verbatim.\"\n" +
		"model = \"gpt-6.1-sol\"\n" +
		"model_reasoning_effort = \"high\"\n" +
		"sandbox_mode = \"read-only\"\n" +
		"developer_instructions = \"\"\"\n"
	// The role's own body, escaped byte for byte, is the whole value.
	body := "Body has a literal triple quote \\\"\\\"\\\" and a backslash \\\\ standalone.\n" +
		"\"\"\"\n"
	if got != head+body {
		t.Fatalf("quirky.toml =\n%q\nwant %q ... %q", got, head, body)
	}
	if err := codex.ValidateRole(got); err != nil {
		t.Fatalf(
			"emitted TOML does not parse (the exact startup failure this escaping exists to prevent): %v\n%s",
			err,
			got,
		)
	}
}

func TestGlobalAgentsUnquotesYAMLQuotedDescription(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(
		t,
		filepath.Join(home, "source-repo", "agents", "quoted.md"),
		"---\nname: quoted\ndescription: 'a: b, \"c\"'\n---\n\nBody.\n",
	)

	if _, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home}); err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	got := string(readAgentFixture(
		t,
		filepath.Join(filepath.Join(home, ".codex", "agents"), "quoted.toml"),
	))
	head := ("# " + artifact.Prefix) + "agents/quoted.md" +
		" — edit the source, then run: engine-sync build\n" +
		"name = \"quoted\"\n" +
		"description = \"a: b, \\\"c\\\"\"\n" +
		"developer_instructions = \"\"\"\n"
	if !strings.HasPrefix(got, head) || !strings.HasSuffix(got, "\nBody.\n\"\"\"\n") {
		t.Fatalf("quoted.toml =\n%q\nwant %q ... %q", got, head, "Body.")
	}
}

// TestGlobalAgentSourcesCompileDeterministicallyToValidTOML replaces the old
// tracked-twin comparison: wave 9b retired the tracked `.toml` twins from
// git (root CLAUDE.md — "nothing generated is written into the clone"), so
// there is no on-disk artifact left to diff against. What remains true and
// worth pinning for every `agents/*.md` shape: the
// compiler succeeds, is deterministic (two renders byte-equal), the output
// independently parses as TOML (via the same BurntSushi/toml parser
// validateTOML already depends on — no new dependency), carries the
// source frontmatter's name/description, and no `.toml` is ever written
// beside the `.md` source (the clone stays clean).
func TestGlobalAgentSourcesCompileDeterministicallyToValidTOML(t *testing.T) {
	// The professor clone's real roster is compiled by its own gate (the
	// fence runs engine-sync over the checkout); this repository pins every
	// shape that roster uses on an invented fixture roster.
	agentsDir := filepath.Join("testdata", "global-agents")
	sources, err := filepath.Glob(filepath.Join(agentsDir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatalf("no global agent sources in %s", agentsDir)
	}
	for _, source := range sources {
		t.Run(strings.TrimSuffix(filepath.Base(source), ".md"), func(t *testing.T) {
			raw, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			fields, _, err := parseFrontmatter(string(raw))
			if err != nil {
				t.Fatalf("parse frontmatter of %s: %v", source, err)
			}
			wantName := strings.TrimSpace(fields["name"])
			// The description reaches the compiled role verbatim but for the
			// one rewrite a Codex lead needs: Claude's /code-review names a
			// command no Codex seat has (review.go).
			wantDescription := rewriteCodeReview(strings.TrimSpace(fields["description"]), fixtureMachinePolicy().Codex.ModelMap, fixtureMachinePolicy().Codex.Review)

			_, first, err := renderFixtureMachineAgent(source, string(raw), agentsDir)
			if err != nil {
				t.Fatalf("renderGlobalAgentTOML: %v", err)
			}
			_, second, err := renderFixtureMachineAgent(source, string(raw), agentsDir)
			if err != nil {
				t.Fatalf("renderGlobalAgentTOML (second render): %v", err)
			}
			if first != second {
				t.Fatalf("renderGlobalAgentTOML is not deterministic:\nfirst:\n%q\nsecond:\n%q", first, second)
			}
			if strings.Contains(first, "model = \"claude-") {
				t.Fatalf("Claude model ID in Codex role:\n%s", first)
			}

			var document struct {
				Name        string `toml:"name"`
				Description string `toml:"description"`
			}
			if _, err := toml.Decode(first, &document); err != nil {
				t.Fatalf("rendered TOML does not parse: %v\n%s", err, first)
			}
			if document.Name != wantName {
				t.Fatalf("rendered TOML name = %q, want %q (frontmatter)", document.Name, wantName)
			}
			if document.Description != wantDescription {
				t.Fatalf("rendered TOML description = %q, want %q (frontmatter)", document.Description, wantDescription)
			}

			besideSource := strings.TrimSuffix(source, ".md") + ".toml"
			if _, err := os.Lstat(besideSource); !os.IsNotExist(err) {
				t.Fatalf("expected no .toml written beside the source at %s, lstat err=%v", besideSource, err)
			}
		})
	}
}

// TestGlobalAgentEscapeMirrorsBuildCodexMJS pins the basic-string escape to
// build-codex.mjs:151 exactly: backslash doubled, then a raw quote escaped.
// Order matters — escaping the quote first would double-escape the
// backslashes it just inserted.
func TestGlobalAgentEscapeMirrorsBuildCodexMJS(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"raw quote pair", `Uses "walker fast" and "map it now".`, `Uses \"walker fast\" and \"map it now\".`},
		{"backslash before quote", `back\slash "then" quote`, `back\\slash \"then\" quote`},
		{"plain text unchanged", `no special characters here`, `no special characters here`},
	}
	for _, c := range cases {
		if got := codex.EscapeRole(c.in); got != c.want {
			t.Errorf("%s: codex.EscapeRole(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestGlobalAgentEscapeMultilineNeutralisesTripleQuoteCollision pins the
// multi-line basic-string escape to build-codex.mjs:153 exactly: backslash
// doubled, then a literal `"""` neutralised so it can never be mistaken for
// the string's own closing delimiter.
func TestGlobalAgentEscapeMultilineNeutralisesTripleQuoteCollision(t *testing.T) {
	in := "line one\nhas a literal \"\"\" inside\nand a backslash \\ alone"
	want := "line one\nhas a literal \\\"\\\"\\\" inside\nand a backslash \\\\ alone"
	if got := codex.EscapeRoleMultiline(in); got != want {
		t.Fatalf("codex.EscapeRoleMultiline(%q) = %q, want %q", in, got, want)
	}
}

func TestGlobalAgentsAcceptsEmptySourceRegistry(t *testing.T) {
	home := testjail.Scratch(t)
	if err := os.MkdirAll(filepath.Join(home, "source-repo", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("empty source registry: %v", err)
	}
}

// TestGlobalAgentsCheckReportsMissingBeforeInstall is the RED-then-GREEN pin
// on check mode's classification: nothing has been installed yet, so every
// desired target must classify as missing — never fabricated as "installed"
// just because check mode looked.
func TestGlobalAgentsCheckReportsMissingBeforeInstall(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")
	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home, Mode: ModeCheck})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Installed) != 1 || len(result.Roles) != 1 {
		t.Fatalf("installed=%#v roles=%#v, want one of each desired target", result.Installed, result.Roles)
	}
	if result.Installed[0].State != GlobalLinkMissing {
		t.Fatalf(
			"check mode classified an absent link as %s, not missing: %#v",
			result.Installed[0].State,
			result.Installed[0],
		)
	}
	if result.Roles[0].State != GlobalRoleMissing {
		t.Fatalf("check mode classified an absent role as %s, not missing: %#v", result.Roles[0].State, result.Roles[0])
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", "agents", "alpha.md"),
		filepath.Join(home, ".codex", "agents", "alpha.toml"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("check mode wrote %s: %v", path, err)
		}
	}
}

// TestGlobalAgentsCodexRosterNilDefaultsEmptyPlansNoRole pins the roster
// contract the installer relies on: a nil CodexHomes (`pfm codex agents`)
// defaults to {Home}/.codex, while a non-nil empty CodexHomes (an install
// with no Codex account) means no Codex home — the Claude link is still
// planned and built, and no role is planned or written.
func TestGlobalAgentsCodexRosterNilDefaultsEmptyPlansNoRole(t *testing.T) {
	for _, tc := range []struct {
		name      string
		roster    []string
		wantRoles int
	}{
		{name: "nil roster", roster: nil, wantRoles: 1},
		{name: "empty roster", roster: []string{}, wantRoles: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := testjail.Scratch(t)
			source := filepath.Join(home, "source-repo", "agents", "alpha.md")
			writeAgentFixture(t, source, "---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")
			options := GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home, CodexHomes: tc.roster, Mode: ModeCheck}
			plan, err := runFixtureMachineAgents(options)
			if err != nil {
				t.Fatalf("RunGlobalAgents check: %v", err)
			}
			if len(plan.Roles) != tc.wantRoles {
				t.Fatalf("planned roles=%#v, want %d", plan.Roles, tc.wantRoles)
			}
			if len(plan.Installed) != 1 {
				t.Fatalf("planned Claude links=%#v, want one", plan.Installed)
			}
			options.Mode = ModeBuild
			if _, err := runFixtureMachineAgents(options); err != nil {
				t.Fatalf("RunGlobalAgents build: %v", err)
			}
			assertGlobalSymlink(t, filepath.Join(home, ".claude", "agents", "alpha.md"), source)
			role := filepath.Join(home, ".codex", "agents", "alpha.toml")
			if tc.wantRoles == 0 {
				if _, err := os.Lstat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
					t.Fatalf("empty roster wrote under ~/.codex: %v", err)
				}
				return
			}
			assertGlobalRoleFile(t, role)
		})
	}
}

// TestGlobalAgentsInstallLinksClaudeAndWritesTheCodexRole pins the two shapes
// apart: Claude's registry entry must be a symlink resolving to the
// source-repo original, and Codex's must be the regular role file its loader
// can actually open.
func TestGlobalAgentsInstallLinksClaudeAndWritesTheCodexRole(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")

	if _, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home}); err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}

	assertGlobalSymlink(t,
		filepath.Join(home, ".claude", "agents", "alpha.md"),
		filepath.Join(home, "source-repo", "agents", "alpha.md"))
	assertGlobalRoleFile(t, filepath.Join(home, ".codex", "agents", "alpha.toml"))
}

// TestGlobalAgentsInstallReplacesAnOwnedCopyWithASymlink covers the exact
// migration case this rewrite exists for: a regular-file copy the old
// copy-based installer left behind at the desired path is ours (its basename
// IS the roster entry) and gets replaced with the link, not backed up as a
// stranger's file.
func TestGlobalAgentsInstallReplacesAnOwnedCopyWithASymlink(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")
	writeAgentFixture(t, filepath.Join(home, ".claude", "agents", "alpha.md"), artifact.Marker(artifact.Markdown, "alpha.md")+"\nstale copy from the old installer\n")

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	if len(result.Problems) != 0 {
		t.Fatalf("a legacy copy at our own desired path was reported as a conflict: %#v", result.Problems)
	}
	assertGlobalSymlink(t,
		filepath.Join(home, ".claude", "agents", "alpha.md"),
		filepath.Join(home, "source-repo", "agents", "alpha.md"))
}

// TestGlobalAgentsPreservesHandmadeInRepoSymlink covers a symlink that
// already points somewhere INSIDE the source repository, just not at the
// current desired source. Without a creation receipt it is foreign,
// preserved and reported as a conflict.
func TestGlobalAgentsPreservesHandmadeInRepoSymlink(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")
	staleSource := filepath.Join(home, "source-repo", "templates", "global", "retired-alpha.md")
	writeAgentFixture(t, staleSource, "a since-renamed agent source\n")
	claudeDest := filepath.Join(home, ".claude", "agents")
	if err := os.MkdirAll(claudeDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(staleSource, filepath.Join(claudeDest, "alpha.md")); err != nil {
		t.Fatal(err)
	}

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	if len(result.Problems) == 0 {
		t.Fatalf("a stale in-repo symlink was not reported as a conflict: %#v", result.Problems)
	}
	assertGlobalSymlink(t,
		filepath.Join(home, ".claude", "agents", "alpha.md"),
		staleSource)
}

// TestGlobalAgentsPreservesHandmadeInCloneRoleLink covers a hand-made link
// to an old in-clone TOML. Its location does not establish ownership: without
// a creation receipt it is preserved and reported as a conflict.
func TestGlobalAgentsPreservesHandmadeInCloneRoleLink(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")
	oldInCloneTOML := filepath.Join(home, "source-repo", "agents", "alpha.toml")
	writeAgentFixture(t, oldInCloneTOML, "name = \"alpha\"\ndescription = \"stale tracked twin\"\n")
	codexDest := filepath.Join(home, ".codex", "agents")
	if err := os.MkdirAll(codexDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldInCloneTOML, filepath.Join(codexDest, "alpha.toml")); err != nil {
		t.Fatal(err)
	}

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	if len(result.Problems) == 0 {
		t.Fatalf("a hand-made link to the old in-clone twin was not reported as a conflict: %#v", result.Problems)
	}
	assertGlobalSymlink(t, filepath.Join(home, ".codex", "agents", "alpha.toml"), oldInCloneTOML)
}

// TestGlobalAgentsInstallLeavesAForeignSymlinkAlone is the conflict-law pin:
// a symlink pointing OUTSIDE the source repository entirely — an operator's
// own file, nothing this installer ever wrote — is never overwritten, never
// deleted, and is reported by exact CONFLICT wording rather than silently
// skipped.
func TestGlobalAgentsInstallLeavesAForeignSymlinkAlone(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")

	claudeDest := filepath.Join(home, ".claude", "agents")
	if err := os.MkdirAll(claudeDest, 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(home, "elsewhere.md")
	writeAgentFixture(t, elsewhere, "an operator's own file, unrelated to the source repo\n")
	foreignLink := filepath.Join(claudeDest, "alpha.md")
	if err := os.Symlink(elsewhere, foreignLink); err != nil {
		t.Fatal(err)
	}

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	want := "CONFLICT " + foreignLink + ": not ours (points to " + elsewhere + ")"
	found := false
	for _, problem := range result.Problems {
		if problem == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("problems = %#v, want to contain %q", result.Problems, want)
	}

	resolved, err := os.Readlink(foreignLink)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != elsewhere {
		t.Fatalf("foreign symlink was rewritten: now -> %s, want -> %s", resolved, elsewhere)
	}
}

// TestGlobalAgentsRepointsAReceiptlessLinkToAGeneratedFile: a registry link
// with no receipt that points at a file carrying engine-sync's marker — left by
// an installer that wrote no receipt — is ours, so a moved render repoints it.
func TestGlobalAgentsRepointsAReceiptlessLinkToAGeneratedFile(t *testing.T) {
	home := testjail.Scratch(t)
	source := filepath.Join(home, "source-repo", "agents", "alpha.md")
	writeAgentFixture(t, source, "---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")

	claudeDest := filepath.Join(home, ".claude", "agents")
	if err := os.MkdirAll(claudeDest, 0o755); err != nil {
		t.Fatal(err)
	}
	oldRender := filepath.Join(home, "old-store", "alpha.md")
	writeAgentFixture(t, oldRender, "---\n# Generated by engine-sync from agents/alpha.md — edit the source, then run: engine-sync build\nname: alpha\n---\n\nbody\n")
	link := filepath.Join(claudeDest, "alpha.md")
	if err := os.Symlink(oldRender, link); err != nil {
		t.Fatal(err)
	}

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	for _, problem := range result.Problems {
		if strings.Contains(problem, "CONFLICT "+link) {
			t.Fatalf("a receipt-less link to a generated file was refused: %q", problem)
		}
	}
	assertGlobalSymlink(t, link, source)
}

// TestGlobalAgentsLinkIntoEveryConfiguredClaudeConfigDir pins the fanout the
// retire side already had: a host with two configured Claude accounts must
// receive one agent link per account registry, not one link into the primary
// while every other account silently has no global agents at all. Check mode
// plans a link for BOTH dirs, build creates both, and a second check reports
// nothing left to do.
func TestGlobalAgentsLinkIntoEveryConfiguredClaudeConfigDir(t *testing.T) {
	home := testjail.Scratch(t)
	source := filepath.Join(home, "source-repo", "agents", "alpha.md")
	writeAgentFixture(t, source, "---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")
	first := filepath.Join(home, ".claude")
	second := filepath.Join(home, ".claude2")
	options := GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home, ClaudeConfigDirs: []string{first, second}}

	options.Mode = ModeCheck
	plan, err := runFixtureMachineAgents(options)
	if err != nil {
		t.Fatalf("RunGlobalAgents check: %v", err)
	}
	for _, want := range []string{
		filepath.Join(first, "agents", "alpha.md"),
		filepath.Join(second, "agents", "alpha.md"),
	} {
		if !hasGlobalAgentLinkAction(plan.Actions, want, source) {
			t.Fatalf("check mode planned no link for %s: %#v", want, plan.Actions)
		}
	}

	options.Mode = ModeBuild
	if _, err := runFixtureMachineAgents(options); err != nil {
		t.Fatalf("RunGlobalAgents build: %v", err)
	}
	assertGlobalSymlink(t, filepath.Join(first, "agents", "alpha.md"), source)
	assertGlobalSymlink(t, filepath.Join(second, "agents", "alpha.md"), source)

	options.Mode = ModeCheck
	settled, err := runFixtureMachineAgents(options)
	if err != nil {
		t.Fatalf("RunGlobalAgents recheck: %v", err)
	}
	if len(settled.Actions) != 0 || len(settled.Problems) != 0 {
		t.Fatalf("a settled install still reports work: actions=%#v problems=%#v", settled.Actions, settled.Problems)
	}
}

// TestGlobalAgentsPreservesAliasedAccountRegistry is the real host shape:
// the second account's agents/ registry is itself a directory symlink into
// the first account's registry. Both desired targets resolve to the same
// physical file, so build must converge with no error and a recheck must
// report zero actions and zero problems — never a CONFLICT, and never an
// "file exists" failure from linking the same physical path twice.
func TestGlobalAgentsPreservesAliasedAccountRegistry(t *testing.T) {
	home := testjail.Scratch(t)
	source := filepath.Join(home, "source-repo", "agents", "alpha.md")
	writeAgentFixture(t, source, "---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nbody\n")
	first := filepath.Join(home, ".claude")
	second := filepath.Join(home, ".claude2")
	if err := os.MkdirAll(filepath.Join(first, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(first, "agents"), filepath.Join(second, "agents")); err != nil {
		t.Fatal(err)
	}
	options := GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home, ClaudeConfigDirs: []string{first, second}, Mode: ModeBuild}

	result, err := runFixtureMachineAgents(options)
	if err != nil || len(result.Problems) == 0 {
		t.Fatalf("aliased directory must conflict: %#v err=%v", result, err)
	}
	if target, err := os.Readlink(filepath.Join(second, "agents")); err != nil || target != filepath.Join(first, "agents") {
		t.Fatalf("directory alias changed: %q %v", target, err)
	}
}

// TestGlobalAgentsInstallsCodexRoleAsRegularFile is the regression for the
// defect that made every machine-global role unspawnable on Codex ≥0.154:
// the role loader reads a role file through read_sensitive_file_to_string
// (codex-rs/exec-server/src/regular_file.rs), which opens with O_NOFOLLOW and
// rejects a symlink outright, and codex-rs/core/src/agent/role.rs renders that
// rejection as the single vague line "agent type is currently not available".
// A symlink at {codex home}/agents/<name>.toml is therefore not an installed
// role at all — it is a role Codex refuses to load.
func TestGlobalAgentsInstallsCodexRoleAsRegularFile(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nAlpha body.\n")

	if _, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home}); err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}

	role := filepath.Join(home, ".codex", "agents", "alpha.toml")
	info, err := os.Lstat(role)
	if err != nil {
		t.Fatalf("expected an installed role at %s: %v", role, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s mode=%v — Codex rejects anything but a regular file", role, info.Mode())
	}
	assertAgentContains(t, role, ("# " + artifact.Prefix), `name = "alpha"`)
}

// TestGlobalAgentsPreservesUnprovenLegacyRoleLink covers the upgrade
// path every host that ever ran the symlinking installer is on: the link is
// pfm's own, so it is replaced by the real file rather than reported as a
// conflict.
func TestGlobalAgentsPreservesUnprovenLegacyRoleLink(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nAlpha body.\n")
	legacy := filepath.Join(paths.LegacyGeneratedCodexAgentsDir(home), "alpha.toml")
	writeAgentFixture(t, legacy, "name = \"alpha\"\n")
	role := filepath.Join(home, ".codex", "agents", "alpha.toml")
	if err := os.MkdirAll(filepath.Dir(role), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(legacy, role); err != nil {
		t.Fatal(err)
	}

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	if len(result.Problems) == 0 {
		t.Fatalf("handmade role link must conflict: %#v", result)
	}
	assertGlobalSymlink(t, role, legacy)
}

// TestGlobalAgentsRefusesForeignCodexRoleFile is the other half of ownership:
// a regular file of the same name that pfm did not write carries no generated
// marker, so it is reported and left byte-for-byte alone. Overwriting it
// would destroy an operator's own role.
func TestGlobalAgentsRefusesForeignCodexRoleFile(t *testing.T) {
	home := testjail.Scratch(t)
	writeAgentFixture(t, filepath.Join(home, "source-repo", "agents", "alpha.md"),
		"---\nname: alpha\ndescription: Alpha role for testing.\n---\n\nAlpha body.\n")
	role := filepath.Join(home, ".codex", "agents", "alpha.toml")
	const foreign = "name = \"alpha\"\ndescription = \"hand-written by the operator\"\n"
	writeAgentFixture(t, role, foreign)

	result, err := runFixtureMachineAgents(GlobalAgentsOptions{SourceRepo: filepath.Join(home, "source-repo"), Home: home})
	if err != nil {
		t.Fatalf("RunGlobalAgents: %v", err)
	}
	if got := string(readAgentFixture(t, role)); got != foreign {
		t.Fatalf("%s was overwritten:\n%s", role, got)
	}
	if len(result.Problems) == 0 {
		t.Fatalf("a foreign role file was silently accepted: problems = %#v", result.Problems)
	}
}

// A global role compiles to the same tools-derived sandbox and model pin as a
// project role: read-only unless its tools grant a write tool or it is gitter,
// and model emitted whether or not effort is set.
func TestGlobalAgentTOMLSandboxAndModelPin(t *testing.T) {
	cases := []struct {
		file, frontmatter string
		want, wantAbsent  []string
	}{
		{
			file:        "tracer-rr.md",
			frontmatter: "name: tracer-rr\ndescription: Reads.\ntools: Bash, Read\nmodel: opus\nengines:\n  codex:\n    sandbox: workspace-write\n",
			want:        []string{"model = \"gpt-6.1-sol\"\n", "sandbox_mode = \"workspace-write\"\n"},
		},
		{
			file:        "scout.md",
			frontmatter: "name: scout\ndescription: Reads.\ntools: Read, Grep, Glob, Bash\n",
			want:        []string{"sandbox_mode = \"read-only\"\n"},
		},
		{
			file:        "gitter.md",
			frontmatter: "name: gitter\ndescription: Commits.\ntools: Read, Grep, Glob, Bash\nengines:\n  codex:\n    sandbox: inherit\n",
			wantAbsent:  []string{"sandbox_mode"},
		},
		{
			file:        "writer.md",
			frontmatter: "name: writer\ndescription: Writes.\ntools: Read, Edit, Bash\n",
			wantAbsent:  []string{"sandbox_mode"},
		},
		{
			file:        "untooled.md",
			frontmatter: "name: untooled\ndescription: Everything.\n",
			wantAbsent:  []string{"sandbox_mode"},
		},
		{
			file:        "pinned.md",
			frontmatter: "name: pinned\ndescription: Reviews.\ntools: Read, Grep\nmodel: sonnet\n",
			want: []string{
				"model = \"" + fixtureMachinePolicy().Codex.ModelMap["sonnet"] + "\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions",
			},
			wantAbsent: []string{"model_reasoning_effort"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			agentsDir := testjail.Scratch(t)
			source := filepath.Join(agentsDir, tc.file)
			_, got, err := renderFixtureMachineAgent(source, "---\n"+tc.frontmatter+"---\n\nBody.\n", agentsDir)
			if err != nil {
				t.Fatalf("renderGlobalAgentTOML: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("%s TOML lacks %q:\n%s", tc.file, want, got)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Fatalf("%s TOML must carry no %q:\n%s", tc.file, absent, got)
				}
			}
			if err := codex.ValidateRole(got); err != nil {
				t.Fatalf("emitted TOML does not parse: %v\n%s", err, got)
			}
		})
	}
}
