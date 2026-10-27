package opencode

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func writeOpenCodeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsProblem(problems []string, want string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, want) {
			return true
		}
	}
	return false
}

func TestBuildCheckDoctorCompileOpenCodeTree(t *testing.T) {
	root := testjail.Scratch(t)
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"opencode":{"modelMap":{"sonnet":"openai/gpt-6.1-sol-fast"},"mirrorFiles":["LICENSE","SECURITY.md"]}}`)
	home := filepath.Join(root, "home")
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "agents", "worker.md"),
		"---\ndescription: Worker role.\nmodel: sonnet # tier comment\ntools: Read, Bash, Agent\n---\nUse /tools:review.\n",
	)
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "agents", "unconfigured.md"),
		"---\ndescription: Unconfigured role.\n---\nUse the defaults.\n",
	)
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "commands", "tools", "review.md"),
		"---\ndescription: Review.\n---\nReview the change.\n",
	)
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "skills", "context-meter", "SKILL.md"),
		"---\ndescription: Meter.\n---\nCount context.\n",
	)
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".mcp.json"),
		`{"mcpServers":{"local":{"command":"app","args":["mcp","serve"],"env":{"X":"1"}},"remote":{"url":"http://127.0.0.1:9/mcp"}}}`,
	)
	writeOpenCodeFixture(t, filepath.Join(root, "LICENSE"), "license\n")
	writeOpenCodeFixture(t, filepath.Join(root, "SECURITY.md"), "security\n")
	writeOpenCodeFixture(
		t,
		filepath.Join(home, ".claude", "commands", "global.md"),
		"---\ndescription: Global.\n---\nGlobal.\n",
	)

	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("build result=%#v err=%v", result, err)
	}
	for _, path := range []string{
		filepath.Join(root, ".opencode", "agent", "worker.md"),
		filepath.Join(root, ".opencode", "command", "tools-review.md"),
		filepath.Join(root, ".opencode", "skills", "context-meter"),
		filepath.Join(root, ".opencode", "opencode.jsonc"),
		filepath.Join(root, ".opencode", "LICENSE"),
		filepath.Join(root, ".opencode", "SECURITY.md"),
		filepath.Join(home, ".config", "opencode", "command", "global.md"),
	} {
		if _, statErr := os.Lstat(path); statErr != nil {
			t.Fatalf("build missing %s: %v", path, statErr)
		}
	}
	worker, _ := os.ReadFile(filepath.Join(root, ".opencode", "agent", "worker.md"))
	if !strings.Contains(string(worker), "mode: all") ||
		!strings.Contains(string(worker), `model: "openai/gpt-6.1-sol-fast"`) ||
		!strings.Contains(string(worker), "tools:\n") ||
		!strings.Contains(string(worker), "  edit: false\n") ||
		strings.Contains(string(worker), "  read: false\n") ||
		strings.Contains(string(worker), "  bash: false\n") ||
		strings.Contains(string(worker), "  task: false\n") ||
		!strings.Contains(string(worker), "/tools-review") ||
		strings.Contains(string(worker), "Model tier:") {
		t.Fatalf("agent projection did not follow OpenCode shape: %s", worker)
	}
	unconfigured, _ := os.ReadFile(filepath.Join(root, ".opencode", "agent", "unconfigured.md"))
	unconfiguredFrontmatter := strings.SplitN(string(unconfigured), "---", 3)[1]
	if strings.Contains(unconfiguredFrontmatter, "\nmodel:") || strings.Contains(unconfiguredFrontmatter, "\ntools:") {
		t.Fatalf("agent without source model/tools acquired policy keys: %s", unconfigured)
	}
	command, _ := os.ReadFile(filepath.Join(root, ".opencode", "command", "tools-review.md"))
	commandText := string(command)
	if !strings.Contains(commandText, "description: \"Review.\"") || strings.Contains(commandText, "name:") ||
		strings.Contains(commandText, "allowed-tools:") ||
		strings.Contains(commandText, "argument-hint:") {
		t.Fatalf("command projection leaked Claude-only frontmatter: %s", commandText)
	}
	configRaw, _ := os.ReadFile(filepath.Join(root, ".opencode", "opencode.jsonc"))
	var config map[string]any
	if err := json.Unmarshal(parseOpenCodeJSONC(configRaw), &config); err != nil {
		t.Fatalf("generated config is not JSON: %v", err)
	}
	if _, ok := config["mcp"]; !ok {
		t.Fatalf("generated config omitted MCP mapping: %s", configRaw)
	}
	check, err := Compile(Options{Root: root, Home: home, Mode: ModeCheck})
	if err != nil || !check.OK {
		t.Fatalf("check result=%#v err=%v", check, err)
	}
	doctor, err := Compile(Options{Root: root, Home: home, Mode: ModeDoctor})
	if err != nil || !doctor.OK {
		t.Fatalf("doctor result=%#v err=%v", doctor, err)
	}
}

func TestOpenCodeAgentModelOverrideWinsOverDefault(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "agents", "worker.md"),
		"---\ndescription: Worker role.\nmodel: sonnet\n---\nWork.\n",
	)
	writeOpenCodeFixture(
		t,
		filepath.Join(root, openCodeBuildConfigPath),
		`{"modelMap":{"sonnet":"openai/other"}}`,
	)

	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("build result=%#v err=%v", result, err)
	}
	worker, err := os.ReadFile(filepath.Join(root, ".opencode", "agent", "worker.md"))
	if err != nil || !strings.Contains(string(worker), "model: \"openai/other\"\n") {
		t.Fatalf("override was not emitted: worker=%q err=%v", worker, err)
	}
}

func TestOpenCodeUnmappedModelAliasIsOmittedWithAWarning(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"opencode":{"modelMap":{"opus":"openai/gpt-6.1-sol"}}}`)
	unmapped := filepath.Join(root, ".claude", "agents", "worker.md")
	writeOpenCodeFixture(t, unmapped, "---\ndescription: Worker role.\nmodel: unmapped-tier\n---\nWork.\n")
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "agents", "lead.md"),
		"---\ndescription: Lead role.\nmodel: opus\n---\nLead.\n",
	)

	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("build result=%#v err=%v", result, err)
	}
	want := `unmapped OpenCode model alias "unmapped-tier" in ` + unmapped + " — model omitted"
	if !containsProblem(result.Warnings, want) {
		t.Fatalf("warnings = %q, want %q", result.Warnings, want)
	}
	worker, err := os.ReadFile(filepath.Join(root, ".opencode", "agent", "worker.md"))
	if err != nil || strings.Contains(strings.SplitN(string(worker), "---", 3)[1], "\nmodel:") {
		t.Fatalf("unmapped agent: worker=%q err=%v", worker, err)
	}
	lead, err := os.ReadFile(filepath.Join(root, ".opencode", "agent", "lead.md"))
	if err != nil || !strings.Contains(string(lead), "model: \"openai/gpt-6.1-sol\"\n") {
		t.Fatalf("mapped agent: lead=%q err=%v", lead, err)
	}
}

func TestOpenCodeUnmarkedConfigFailsAndIsLeftAloneInEveryMode(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	config := filepath.Join(root, ".opencode", "opencode.jsonc")
	const handWritten = "{\n  \"model\": \"mine\"\n}\n"
	writeOpenCodeFixture(t, config, handWritten)
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "agents", "worker.md"), "---\ndescription: Worker.\n---\nWork.\n")

	for _, mode := range []Mode{ModeBuild, ModeCheck, ModeDoctor} {
		result, err := Compile(Options{Root: root, Home: home, Mode: mode})
		if err != nil || result.OK || !containsProblem(result.Problems, "CONFLICT "+config) {
			t.Fatalf("mode %d result=%#v err=%v", mode, result, err)
		}
		got, err := os.ReadFile(config)
		if err != nil || string(got) != handWritten {
			t.Fatalf("mode %d touched the unmarked config: %q err=%v", mode, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".opencode", "agent", "worker.md")); err != nil {
		t.Fatalf("build skipped the agent beside an unmarked config: %v", err)
	}
}

func TestOpenCodeArchivedAgentIsNotCompiled(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "agents", "worker.md"), "---\ndescription: Worker.\n---\nWork.\n")
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "agents", "archive", "old.md"),
		"---\ndescription: Old.\n---\nOld.\n",
	)

	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("build result=%#v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".opencode", "agent", "worker.md")); err != nil {
		t.Fatalf("top-level agent missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".opencode", "agent", "old.md")); !os.IsNotExist(err) {
		t.Fatalf("archived agent was compiled: err=%v", err)
	}
}

func TestOpenCodeMalformedModelOverrideIsAProblem(t *testing.T) {
	root := testjail.Scratch(t)
	home := filepath.Join(root, "home")
	path := filepath.Join(root, openCodeBuildConfigPath)
	writeOpenCodeFixture(t, path, `{"modelMap":`)

	result, err := Compile(Options{Root: root, Home: home, Mode: ModeCheck})
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("malformed override result=%#v err=%v", result, err)
	}
}

func TestCheckReportsUnmarkedConflictAndBuildPreservesIt(t *testing.T) {
	root := testjail.Scratch(t)
	path := filepath.Join(root, ".opencode", "command", "review.md")
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "commands", "review.md"),
		"---\ndescription: Review.\n---\nReview.\n",
	)
	writeOpenCodeFixture(t, path, "hand authored\n")
	result, err := Compile(Options{Root: root, Home: filepath.Join(root, "home"), Mode: ModeBuild})
	if err != nil || result.OK || !containsProblem(result.Problems, "CONFLICT") {
		t.Fatalf("unmarked conflict was not refused: result=%#v err=%v", result, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hand authored\n" {
		t.Fatalf("conflict was overwritten: %q", got)
	}
}

func TestOpenCodeWorkbenchParentProjects(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"workbenchMarker":".example/workbench.json"}`)
	writeOpenCodeFixture(t, filepath.Join(root, "scribe", "CLAUDE.md"), "Scribe.\n")
	writeOpenCodeFixture(t, filepath.Join(root, "scribe", ".example", "workbench.json"), `{}`)
	writeOpenCodeFixture(
		t,
		filepath.Join(root, "scribe", ".claude", "agents", "clerk.md"),
		"---\ndescription: Clerk.\n---\nClerk.\n",
	)
	writeOpenCodeFixture(t, filepath.Join(root, "api", "CLAUDE.md"), "API.\n")
	writeOpenCodeFixture(
		t,
		filepath.Join(root, "api", ".claude", "agents", "clerk.md"),
		"---\ndescription: Clerk.\n---\nClerk.\n",
	)
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("build=%#v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".opencode", "agent", "clerk-api.md")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".opencode", "agent", "clerk-scribe.md")); !os.IsNotExist(err) {
		t.Errorf("parent compiled workbench: %v", err)
	}
}

func TestOpenCodeWorkbenchHomeFree(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "engine-sync.json"), `{"version":1,"workbenchMarker":".example/workbench.json"}`)
	writeOpenCodeFixture(t, filepath.Join(root, ".example", "workbench.json"), `{}`)
	writeOpenCodeFixture(
		t,
		filepath.Join(home, ".claude", "commands", "memo.md"),
		"---\ndescription: Memo.\n---\nUse /tools:review.\n",
	)
	writeOpenCodeFixture(
		t,
		filepath.Join(home, ".claude", "commands", "tools", "review.md"),
		"---\ndescription: Review.\n---\nReview.\n",
	)
	writeOpenCodeFixture(
		t,
		filepath.Join(root, ".claude", "commands", "local.md"),
		"---\ndescription: Local.\n---\nUse /tools:review.\n",
	)
	sentinel := filepath.Join(home, ".config", "opencode", "command", "kept.md")
	body := artifact.Prefix + " from fixture\n---\ndescription: \n---\nKept.\n"
	writeOpenCodeFixture(t, sentinel, body)
	for _, mode := range []Mode{ModeBuild, ModeCheck, ModeDoctor} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			result, err := Compile(Options{Root: root, Home: home, Mode: mode})
			if err != nil || !result.OK {
				t.Errorf("mode=%v result=%#v err=%v", mode, result, err)
			}
			if _, err := os.Stat(
				filepath.Join(home, ".config", "opencode", "command", "memo.md"),
			); !os.IsNotExist(
				err,
			) {
				t.Errorf("home memo: %v", err)
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != body {
				t.Errorf("home sentinel=%q err=%v", data, err)
			}
			data, err := os.ReadFile(filepath.Join(root, ".opencode", "command", "local.md"))
			if err != nil || !strings.Contains(string(data), "Use /tools:review.") {
				t.Errorf("unread home roster changed reference: %q err=%v", data, err)
			}
		})
	}
}

func TestOpenCodeWorkbenchProbeErrors(t *testing.T) {
	t.Setenv("ENGINE_SYNC_WORKBENCH_MARKER", ".example/workbench.json")
	for _, scenario := range []string{"root", "child"} {
		t.Run(scenario, func(t *testing.T) {
			root, home := testjail.Scratch(t), testjail.Scratch(t)
			// The kernel's PATH_MAX: Linux 4096, Darwin 1024. The root sits 8
			// bytes under it, the child scenario's 262 under (room for a
			// 255-byte child whose own marker probe then overflows).
			pathMax := 4096
			if runtime.GOOS == "darwin" {
				pathMax = 1024
			}
			length := pathMax - 8
			if scenario == "child" {
				length = pathMax - 262
			}
			for remaining := length - len(root); remaining > 0; {
				n := min(200, remaining-1)
				if remaining-(n+1) == 1 {
					n--
				}
				root = filepath.Join(root, strings.Repeat("d", n))
				remaining -= n + 1
			}
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(home, ".config", "opencode", "command", "old.md")
			body := artifact.Marker(artifact.Markdown, "fixture") + "\n"
			writeOpenCodeFixture(t, sentinel, body)
			var child string
			if scenario == "child" {
				child = filepath.Join(root, strings.Repeat("y", 255))
				if err := os.MkdirAll(child, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
			if scenario == "root" {
				if !errors.Is(err, syscall.ENAMETOOLONG) || !reflect.DeepEqual(result, Result{}) {
					t.Errorf("compile=%#v err=%v, want zero result and ENAMETOOLONG", result, err)
				}
				if raw, err := os.ReadFile(sentinel); err != nil || string(raw) != body {
					t.Errorf("home sentinel=%q err=%v, want %q", raw, err, body)
				}
			} else {
				want := "inspect workbench " + filepath.Join(child, ".example") + ": "
				configPrefix := "config " + filepath.Join(child, ".claude", "engine-sync.json") + ": "
				found := false
				for _, problem := range result.Problems {
					found = found || strings.HasPrefix(problem, want) || strings.HasPrefix(problem, configPrefix)
				}
				if err != nil || result.OK || !found {
					t.Fatalf("compile=%#v err=%v, want problem prefix %q", result, err, want)
				}
			}
		})
	}
}

func TestOpenCodeRealAgentCollidesWithDanglingTwin(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	source := filepath.Join(root, ".claude", "agents", "scan-api.md")
	writeOpenCodeFixture(t, source, "---\ndescription: Old.\n---\nOld.\n")
	result, err := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if err != nil || !result.OK {
		t.Fatalf("seed=%#v err=%v", result, err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../vendor/scan-api.md", source); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeFixture(t, filepath.Join(root, "api", "CLAUDE.md"), "API.\n")
	realSource := filepath.Join(root, "api", ".claude", "agents", "scan.md")
	writeOpenCodeFixture(t, realSource, "---\ndescription: Scan.\n---\nScan.\n")
	result, err = Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	body, readErr := os.ReadFile(filepath.Join(root, ".opencode", "agent", "scan-api.md"))
	if err != nil || result.OK || readErr != nil || !containsProblem(result.Problems, "CONFLICT") || !containsProblem(result.Problems, source) || !containsProblem(result.Problems, realSource) || !strings.Contains(string(body), "Old.") {
		t.Fatalf("collision must preserve incumbent: result=%#v error=%v body=%q read=%v", result, err, body, readErr)
	}
}

func TestReviewDescriptionAndPathClassification(t *testing.T) {
	root := filepath.Join(testjail.Scratch(t), "agent", "project")
	home := testjail.Scratch(t)
	writeOpenCodeFixture(t, filepath.Join(root, "CLAUDE.md"), "Sample\n")
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "commands", "hello.md"), "---\ndescription: Hello.\n---\nHi.\n")
	writeOpenCodeFixture(t, filepath.Join(root, ".claude", "commands", "nodesc.md"), "No description.\n")
	result, e := Compile(Options{Root: root, Home: home, Mode: ModeBuild})
	if e != nil || !result.OK {
		t.Fatalf("one command aborted tree: %+v error=%v", result, e)
	}
	if _, e := os.Stat(filepath.Join(root, ".opencode", "command", "hello.md")); e != nil {
		t.Fatal(e)
	}
}
