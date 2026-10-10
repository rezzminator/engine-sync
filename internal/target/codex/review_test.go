package codex

import (
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"path/filepath"
	"strings"
	"testing"
)

var reviewTestModel = defaultConfig().ModelMap[codeReviewModelAlias]

func codexReviewWant(effort string) string {
	return codexReviewShellCommand(reviewTestModel, effort)
}

func TestRewriteCodeReviewMapsEveryLevelAndLeavesEverythingElse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "low", in: "`/code-review low`", want: "`" + codexReviewWant("low") + "`"},
		{name: "medium", in: "`/code-review medium`", want: "`" + codexReviewWant("medium") + "`"},
		{name: "high", in: "`/code-review high`", want: "`" + codexReviewWant("high") + "`"},
		{name: "xhigh", in: "`/code-review xhigh`", want: "`" + codexReviewWant("xhigh") + "`"},
		{name: "max takes Codex's highest effort", in: "/code-review max", want: codexReviewWant("xhigh")},
		{name: "bare form takes the cheapest", in: "run /code-review now", want: "run " + codexReviewWant("low") + " now"},
		{
			name: "surrounding prose is untouched",
			in:   "Your last step is `/code-review low` over your own change: fix every finding.",
			want: "Your last step is `" + codexReviewWant("low") + "` over your own change: fix every finding.",
		},
		{
			name: "a flag with no codex twin leaves with the invocation",
			in:   "`/code-review high --fix` then report",
			want: "`" + codexReviewWant("high") + "` then report",
		},
		{
			name: "a bare invocation's flag leaves too",
			in:   "/code-review --comment",
			want: codexReviewWant("low"),
		},
		{
			name: "an unknown flag stays visible in the text",
			in:   "/code-review low --whatever",
			want: codexReviewWant("low") + " --whatever",
		},
		{
			name: "a word that merely starts with a level is not a level",
			in:   "/code-review lowish",
			want: codexReviewWant("low") + " lowish",
		},
		{name: "a longer command is not this one", in: "/code-review-x low", want: "/code-review-x low"},
		{name: "a namespaced command is not this one", in: "/code-review:foo", want: "/code-review:foo"},
		{name: "a path is not an invocation", in: "$CDOCS/code-review.md", want: "$CDOCS/code-review.md"},
		{
			name: "a URL is not an invocation",
			in:   "https://example.test/code-review low",
			want: "https://example.test/code-review low",
		},
		{name: "ultra takes Codex's highest effort too", in: "/code-review ultra", want: codexReviewWant("xhigh")},
		{name: "text without the command is returned as it is", in: "no command here", want: "no command here"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := rewriteCodeReview(testCase.in, nil)
			if got != testCase.want {
				t.Fatalf("rewriteCodeReview(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
			if again := rewriteCodeReview(got, nil); again != got {
				t.Fatalf("rewriteCodeReview is not idempotent: second pass = %q, first = %q", again, got)
			}
		})
	}
}

// The lander sizes its own review effort from the diff and reviews the FLIGHT's
// whole diff, not one task's files. Its source line writes the slot form, which
// has to map to a flight-scoped prompt whose model_reasoning_effort is still the
// slot the surrounding prose tells the lander to fill — a baked effort would
// silently review a 40-file flight at the cheapest tier.
func TestRewriteCodeReviewSlotFormIsFlightScopedWithARunTimeEffort(t *testing.T) {
	got := rewriteCodeReview("Run `/code-review "+codeReviewSlot+"` over the flight's diff.", nil)
	want := "Run `" + codexReviewShellCommand(reviewTestModel, codeReviewSlot) + "` over the flight's diff."
	if got != want {
		t.Fatalf("slot form = %q, want %q", got, want)
	}
	if !strings.Contains(got, `model_reasoning_effort="`+codeReviewSlot+`"`) {
		t.Fatalf("slot form baked an effort instead of leaving the slot: %q", got)
	}
	if !strings.Contains(got, codeReviewFlightPrompt) || strings.Contains(got, codeReviewPrompt) {
		t.Fatalf("slot form carries the task-scoped prompt, want the flight-scoped one: %q", got)
	}
	if again := rewriteCodeReview(got, nil); again != got {
		t.Fatalf("slot rewrite is not idempotent: second pass = %q, first = %q", again, got)
	}
	level := rewriteCodeReview("/code-review low", nil)
	if !strings.Contains(level, codeReviewPrompt) || strings.Contains(level, codeReviewSlot) {
		t.Fatalf("a written level must stay task-scoped at a baked effort: %q", level)
	}
}

// The model is the tier map's, so a project whose config maps the review alias
// somewhere else gets ITS model — and a caller with no map of its own (the
// fleet prompt, the global-role compiler) still gets the compiler default
// rather than an empty model codex-cli would reject.
func TestRewriteCodeReviewReadsTheModelFromTheTierMap(t *testing.T) {
	got := rewriteCodeReview("/code-review low", map[string]string{codeReviewModelAlias: "gpt-test-model"})
	if want := codexReviewShellCommand("gpt-test-model", "low"); got != want {
		t.Fatalf("rewrite with a mapped tier = %q, want %q", got, want)
	}
	if model := codeReviewModel(map[string]string{"sonnet": "other"}); model != reviewTestModel {
		t.Fatalf("model for a map without the review alias = %q, want the compiler default %q", model, reviewTestModel)
	}
	if got := rewriteCodeReview("/code-review low", nil); strings.Contains(got, "review_model=") || strings.Contains(got, "-c model=") {
		t.Fatalf("neutral review pinned a model: %s", got)
	}
}

// A command's frontmatter is re-emitted as YAML, and the replacement carries
// double quotes: a quoted scalar has to come back quoted in a style that
// survives them, or the compiled command's frontmatter no longer parses and
// Codex loses the whole command.
func TestCompiledCommandFrontmatterStaysParseableAroundTheReview(t *testing.T) {
	root := testjail.Scratch(t)
	home := testjail.Scratch(t)
	writeCodexFixture(t, filepath.Join(root, "CLAUDE.md"), "# Fixture\n")
	writeCodexFixture(
		t,
		filepath.Join(root, ".claude", "commands", "quoted.md"),
		"---\ndescription: \"Lands it; closes with /code-review low.\"\n---\n\nBody.\n",
	)
	writeCodexFixture(
		t,
		filepath.Join(root, ".claude", "commands", "folded.md"),
		"---\ndescription: >-\n  Lands it; closes\n  with /code-review high.\n---\n\nBody.\n",
	)
	if result, err := RunProject(Options{Root: root, Home: home, Mode: ModeBuild}); err != nil || !result.OK {
		t.Fatalf("build: result=%#v err=%v", result, err)
	}
	for _, command := range []struct{ name, effort string }{{"quoted", "low"}, {"folded", "high"}} {
		t.Run(command.name, func(t *testing.T) {
			path := filepath.Join(root, ".codex", "skills", command.name, "SKILL.md")
			got := string(readCodexFixture(t, path))
			fields, _, err := claudesource.Fields(got)
			if err != nil {
				t.Fatalf("%s no longer parses as frontmatter: %v\n%s", path, err, got)
			}
			if strings.Contains(fields["description"], codeReviewCommand) {
				t.Fatalf("%s description still spells %s: %q", path, codeReviewCommand, fields["description"])
			}
			if !strings.Contains(fields["description"], codexReviewWant(command.effort)) {
				t.Fatalf("%s description carries no %q review: %q", path, command.effort, fields["description"])
			}
		})
	}
}

// Door 3's compiler half: whatever the fleet prompt's parts spell, the
// compose-time mapping turns Claude's review command into Codex's shell
// review. The composed prompt itself is pfm's (its fleet-prompt package
// asserts the composed bytes); this pins the rewrite those bytes go through.
func TestCodeReviewFixtureTailMapsToTheShellReview(t *testing.T) {
	mapped := rewriteCodeReview("- Your last step is `"+codeReviewCommand+" low` over your own change.\n", nil)
	if strings.Contains(mapped, codeReviewCommand) || !strings.Contains(mapped, codexReviewWant("low")) {
		t.Fatalf("the compose-time mapping left a fixture tail unmapped:\n%s", mapped)
	}
}
