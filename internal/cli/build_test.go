package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The public entry points share one neutral project policy, including hooks.
func TestStandaloneProject(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	files := map[string]string{
		"CLAUDE.md":                      "# Sample\nUse /hello.\n",
		".claude/agents/reader.md":       "---\nname: reader\ndescription: Reads files.\ntools: Read, Bash\nmodel: opus\n---\nRead the source.\n",
		".claude/commands/hello.md":      "---\ndescription: Say hello.\n---\nHello.\n",
		".claude/skills/sample/SKILL.md": "---\nname: sample\ndescription: Sample skill.\n---\nRead files.\n",
		".mcp.json":                      `{"mcpServers":{"sample":{"command":"sample-server"}}}`,
	}
	for name, content := range files {
		putProjectFile(t, root, name, content)
	}
	putProjectFile(t, home, ".claude/agents/reader.md", files[".claude/agents/reader.md"])
	vars := map[string]string{"HOME": home, "CLAUDE_PROJECT_DIR": root}
	e := env{get: func(key string) string { return vars[key] }}
	forbidden := []string{"Professor", "professor", "pfm", "gitter", "guarded files", "SECURITY.md", "LICENSE", "codex-build.json"}
	scan := func(label, content string) {
		for _, term := range forbidden {
			if strings.Contains(content, term) {
				t.Errorf("%s contains %q: %s", label, term, content)
			}
		}
	}
	for _, args := range [][]string{{"build", root}, {"check", root}, {"doctor", root}, {"global", "--plan"}, {"hook", "claude", "mark"}, {"hook", "claude", "stop"}, {"hook", "claude", "start"}} {
		var out, errOut bytes.Buffer
		e.stdin = strings.NewReader(`{"tool_input":{"file_path":` + strconvQuote(filepath.Join(root, "CLAUDE.md")) + `}}`)
		if code := dispatch(args, &out, &errOut, e); code != 0 {
			t.Fatalf("%v rc=%d: %s%s", args, code, &out, &errOut)
		}
		scan(strings.Join(args, " "), out.String()+errOut.String())
	}
	for _, dir := range []string{root, home} {
		if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type().IsRegular() {
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				scan(path, string(raw))
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCommandsIgnoreUnrelatedEngineHomeEntries(t *testing.T) {
	for _, entry := range []string{"socket", "unreadable"} {
		t.Run(entry, func(t *testing.T) {
			root, home := testjail.Scratch(t), testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
			vars := map[string]string{"HOME": home, "CLAUDE_PROJECT_DIR": root}
			e := env{get: func(key string) string { return vars[key] }}
			var out, errOut bytes.Buffer
			if code := dispatch([]string{"build", root}, &out, &errOut, e); code != 0 {
				t.Fatalf("fixture build rc=%d: %s%s", code, &out, &errOut)
			}
			path := filepath.Join(home, ".codex", "sessions", "x.jsonl")
			if entry == "socket" {
				path = filepath.Join(home, ".codex", "ipc", "ipc.sock")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				listener, err := net.Listen("unix", path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { listener.Close() })
			} else {
				// A symlink loop is an entry no uid (root included) can read: ELOOP.
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, path); err != nil {
					t.Fatal(err)
				}
				if _, err := os.ReadFile(path); err == nil {
					t.Fatal("fixture does not deny the read")
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"codex", "check", root}, {"build", root}, {"opencode", "check", root}, {"hook", "claude", "stop"}} {
				name := args[0]
				if args[1] != root {
					name = strings.Join(args, " ")
					name = strings.TrimSuffix(name, " "+root)
				}
				t.Run(name, func(t *testing.T) {
					out.Reset()
					errOut.Reset()
					if args[0] == "hook" {
						setFlag(t, e, root)
					}
					if code := dispatch(args, &out, &errOut, e); code != 0 || (args[0] == "hook" && out.Len() != 0) {
						t.Errorf("%v rc=%d: %s%s", args, code, &out, &errOut)
					}
					after, err := os.Lstat(path)
					if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
						t.Errorf("unrelated entry changed: before=%v after=%v err=%v", before, after, err)
					}
				})
			}
		})
	}
}

func TestReviewProjectUsageAndFailures(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	putProjectFile(t, root, "CLAUDE.md", "Sample\n")
	putProjectFile(t, root, "AGENTS.md", "foreign")
	e := env{get: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}}
	for _, args := range [][]string{{"check", "--dry-run", root}, {"doctor", "--dry-run", root}, {"codex", "build", "--engine", "opencode", root}, {"opencode", "build", "--engine", "codex", root}} {
		var out, errOut bytes.Buffer
		if rc := dispatch(args, &out, &errOut, e); rc != 2 || errOut.Len() == 0 {
			t.Errorf("usage %v rc=%d: %s%s", args, rc, &out, &errOut)
		}
	}
	var out, errOut bytes.Buffer
	if rc := dispatch([]string{"plan", root}, &out, &errOut, e); rc != 1 || !strings.Contains(errOut.String(), "CONFLICT") {
		t.Errorf("silent plan rc=%d: %s%s", rc, &out, &errOut)
	}
}

func TestReviewMachinePolicyIndependence(t *testing.T) {
	home := testjail.Scratch(t)
	putProjectFile(t, home, ".claude/commands/hello.md", "---\ndescription: Hello.\n---\nTOKEN /hello.\n")
	e := env{get: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}}
	for _, engine := range []string{"codex", "opencode"} {
		for _, word := range []string{"FIRST", "SECOND"} {
			root := testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
			putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1,"rewrites":[{"literal":"TOKEN","replace":"`+word+`"}]}`)
			var out, errOut bytes.Buffer
			if rc := dispatch([]string{"build", root, "--engine", engine}, &out, &errOut, e); rc != 0 {
				t.Errorf("build rc=%d %s%s", rc, &out, &errOut)
			}
			path := filepath.Join(home, ".codex", "prompts", "hello.md")
			if engine == "opencode" {
				path = filepath.Join(home, ".config", "opencode", "command", "hello.md")
			}
			raw, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(raw), "TOKEN") || strings.Contains(string(raw), word) {
				t.Errorf("project changed machine bytes: %s %v", raw, err)
			}
		}
	}
}

func TestReviewRejectHomeAndCanonicalizeProject(t *testing.T) {
	home := testjail.Scratch(t)
	root := filepath.Join(home, "projects", "sample")
	putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
	e := env{get: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}}
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{home, filepath.Dir(home), alias} {
		var out, errOut bytes.Buffer
		rc := dispatch([]string{"build", path, "--engine", "codex"}, &out, &errOut, e)
		if path == alias {
			if rc != 0 {
				t.Errorf("alias build %d %s", rc, &errOut)
			}
			var cfgOut bytes.Buffer
			if c := dispatch([]string{"config", "validate", alias}, &cfgOut, &errOut, e); c != 0 || !strings.Contains(cfgOut.String(), root) {
				t.Errorf("root differs: %d %s", c, &cfgOut)
			}
		} else if want := "project root " + path + " is HOME or an ancestor of HOME; select a repository"; rc != 1 || !strings.Contains(errOut.String(), want) {
			t.Errorf("unsafe root: rc=%d stdout=%q stderr=%q, want exit 1 and %q", rc, &out, &errOut, want)
		}
	}
}

func TestReviewSharedRoleNaming(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	for _, rel := range []string{"CLAUDE.md", "unit-api/CLAUDE.md"} {
		putProjectFile(t, root, rel, "# Sample\n")
	}
	// Each file base differs from its frontmatter name: the role is named by
	// the frontmatter, the suffix mode applied on top.
	putProjectFile(t, root, "unit-api/.claude/agents/reader-file.md", "---\nname: reader\ndescription: Reads.\n---\nRead.\n")
	putProjectFile(t, root, ".claude/agents/lead-file.md", "---\nname: lead\ndescription: Leads.\n---\nLead.\n")
	putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1,"codex":{"projects":["unit-api"],"suffixMode":"strip-prefix","suffixPrefix":"unit-"}}`)
	e := env{get: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}}
	var out, errOut bytes.Buffer
	if rc := dispatch([]string{"build", root}, &out, &errOut, e); rc != 0 {
		t.Fatalf("build %d %s%s", rc, &out, &errOut)
	}
	for rel, want := range map[string]string{
		".codex/agents/reader-api.toml": `name = "reader-api"`, ".opencode/agent/reader-api.md": "Read.",
		".codex/agents/lead.toml": `name = "lead"`, ".opencode/agent/lead.md": "Lead.",
	} {
		if raw, err := os.ReadFile(filepath.Join(root, rel)); err != nil || !strings.Contains(string(raw), want) {
			t.Errorf("shared naming differs: %s lacks %q (%v)", rel, want, err)
		}
	}
	for _, rel := range []string{".codex/agents/lead-file.toml", ".opencode/agent/reader-file-api.md"} {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			t.Errorf("%s named by its file base", rel)
		}
	}
}

func TestProjectPluginLogOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, action     string
		conflict, asJSON bool
	}{
		{"success", "build", false, false}, {"json-success", "build", false, true},
		{"failure", "build", true, false}, {"json-failure", "build", true, true},
		{"check-failure", "check", true, false}, {"json-check-failure", "check", true, true},
		{"doctor-failure", "doctor", true, false}, {"json-doctor-failure", "doctor", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, home, logs := testjail.Scratch(t), testjail.Scratch(t), testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
			if tc.conflict {
				putProjectFile(t, root, "AGENTS.md", "foreign")
			}
			e := env{sessionID: "s1", get: func(k string) string {
				if k == "HOME" {
					return home
				}
				if k == "PLUGIN_LOG_DIR" {
					return logs
				}
				return ""
			}}
			args := []string{tc.action, root}
			if tc.asJSON {
				args = append(args, "--json")
			}
			var out, stderr bytes.Buffer
			code := dispatch(args, &out, &stderr, e)
			level, event, want := "info", "build-done", 0
			if tc.conflict {
				level, event, want = "errors", tc.action+"-failed", 1
			}
			if code != want {
				t.Fatalf("code%d: %s%s", code, &out, &stderr)
			}
			raw, err := os.ReadFile(filepath.Join(logs, "engine-sync", level+".jsonl"))
			if err != nil {
				t.Fatalf("%s outcome row missing: %v", tc.action, err)
			}
			lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
			if len(lines) != 1 {
				t.Fatalf("outcome count: %s", raw)
			}
			var row map[string]any
			if err := json.Unmarshal(lines[0], &row); err != nil {
				t.Fatal(err)
			}
			rowLevel := "info"
			if tc.conflict {
				rowLevel = "error"
			}
			if row["event"] != event || row["level"] != rowLevel || row["session"] != "s1" || row["where"] != "internal/cli/build.go#runProjectTargets" {
				t.Fatalf("wrong outcome: %s", raw)
			}
			detail, ok := row["detail"].(map[string]any)
			if !ok || detail["code"] != float64(want) {
				t.Fatalf("wrong outcome detail: %s", raw)
			}
			if tc.conflict && (detail["problems"].(float64) < 1 || !strings.Contains(row["msg"].(string), tc.action+" failed") || !strings.Contains(row["msg"].(string), "CONFLICT")) {
				t.Fatalf("failure cause missing: %s", raw)
			}
		})
	}
}

func TestProjectPluginLogWarnings(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run(fmt.Sprint(asJSON), func(t *testing.T) {
			root, home, logs := testjail.Scratch(t), testjail.Scratch(t), testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "Root.\n")
			putProjectFile(t, root, ".claude/agents/orch.md", "---\ndescription: Lab.\nmodel: claude-sonnet-5-5\n---\nLab.\n")
			e := env{get: func(k string) string {
				if k == "HOME" {
					return home
				}
				if k == "PLUGIN_LOG_DIR" {
					return logs
				}
				return ""
			}}
			args := []string{"build", root, "--engine", "codex"}
			if asJSON {
				args = append(args, "--json")
			}
			var out, stderr bytes.Buffer
			if code := dispatch(args, &out, &stderr, e); code != 0 {
				t.Fatalf("code%d: %s%s", code, &out, &stderr)
			}
			raw, err := os.ReadFile(filepath.Join(logs, "engine-sync", "warnings.jsonl"))
			if err != nil {
				t.Fatalf("build warning row missing: %v", err)
			}
			lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
			if len(lines) != 1 {
				t.Fatalf("warning count: %s", raw)
			}
			var row map[string]any
			if err := json.Unmarshal(lines[0], &row); err != nil {
				t.Fatal(err)
			}
			detail, ok := row["detail"].(map[string]any)
			if !ok || row["event"] != "build-warning" || row["where"] != "internal/cli/build.go#runProjectTargets" || row["level"] != "warning" || detail["count"] != float64(1) {
				t.Fatalf("wrong warning: %s", raw)
			}
			if texts, _ := detail["warnings"].([]any); len(texts) != 1 || texts[0] == "" {
				t.Fatalf("warning text missing: %s", raw)
			}
		})
	}
}

// mirrorFiles is every file under root an engine's project mirror owns, by
// its slash path relative to root.
func mirrorFiles(t *testing.T, root, engine string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		owned := strings.HasPrefix(rel, ".opencode/")
		if engine == "codex" {
			owned = rel == "AGENTS.md" || strings.HasPrefix(rel, ".codex/")
		}
		if owned {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[rel] = string(raw)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

// ENGINE_SYNC_DISABLE leaves a disabled engine's stale mirror exactly as it
// is through every project command and the Stop hook, while the enabled
// engine is still checked and rebuilt.
func TestDisabledEnginesLeaveProjectMirrorsAlone(t *testing.T) {
	for _, tc := range []struct {
		name, value       string
		disabled, enabled []string
	}{
		{name: "codex alone", value: "codex", disabled: []string{"codex"}, enabled: []string{"opencode"}},
		{name: "opencode alone", value: " opencode ", disabled: []string{"opencode"}, enabled: []string{"codex"}},
		{name: "both, one repeated", value: "codex, opencode,codex,", disabled: []string{"codex", "opencode"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, home := testjail.Scratch(t), testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
			putProjectFile(t, root, ".claude/commands/hello.md", "---\ndescription: Say hello.\n---\nHello.\n")
			vars := map[string]string{"HOME": home, "CLAUDE_PROJECT_DIR": root}
			e := env{get: func(key string) string { return vars[key] }}
			run := func(stdin string, args ...string) (int, string, string) {
				var out, errOut bytes.Buffer
				e.stdin = strings.NewReader(stdin)
				code := dispatch(args, &out, &errOut, e)
				return code, out.String(), errOut.String()
			}
			if code, out, errOut := run("", "build", root); code != 0 {
				t.Fatalf("fixture build rc=%d: %s%s", code, out, errOut)
			}
			putProjectFile(t, root, "CLAUDE.md", "# Sample, edited\n")
			putProjectFile(t, root, ".claude/commands/bye.md", "---\ndescription: Say bye.\n---\nBye.\n")
			before := map[string]map[string]string{"codex": mirrorFiles(t, root, "codex"), "opencode": mirrorFiles(t, root, "opencode")}
			vars["ENGINE_SYNC_DISABLE"] = tc.value
			skips := func(action string) string {
				rows := ""
				for _, name := range tc.disabled {
					rows += strings.ToUpper(name) + " " + strings.ToUpper(action) + " SKIP disabled by ENGINE_SYNC_DISABLE\n"
				}
				return rows
			}
			untouched := func(step string) {
				t.Helper()
				for _, name := range tc.disabled {
					if got := mirrorFiles(t, root, name); !reflect.DeepEqual(got, before[name]) {
						t.Fatalf("after %s the disabled %s mirror changed:\n got %q\nwant %q", step, name, got, before[name])
					}
				}
			}

			wantCode := 1 // the enabled engine's mirror is stale and still checked
			if len(tc.enabled) == 0 {
				wantCode = 0
			}
			if code, out, errOut := run("", "check", root); code != wantCode || out != skips("check") {
				t.Fatalf("stale check rc=%d stdout %q, want rc=%d stdout %q (stderr %s)", code, out, wantCode, skips("check"), errOut)
			}

			flag := setFlag(t, e, root)
			if code, out, errOut := run("{}", "hook", "claude", "stop"); code != 0 || out != "" {
				t.Fatalf("stop rc=%d stdout %q, want 0 and no answer (stderr %s)", code, out, errOut)
			}
			if _, err := os.Stat(flag); !os.IsNotExist(err) {
				t.Fatalf("stop left the dirty flag %s: %v", flag, err)
			}
			untouched("stop")

			pass := skips("check")
			for _, name := range tc.enabled {
				pass += strings.ToUpper(name) + " CHECK PASS\n"
			}
			if code, out, errOut := run("", "check", root); code != 0 || out != pass {
				t.Fatalf("check after stop rc=%d stdout %q, want 0 and %q (stderr %s)", code, out, pass, errOut)
			}
			if code, out, errOut := run("", "build", root); code != 0 || !strings.HasPrefix(out, skips("build")) {
				t.Fatalf("build rc=%d stdout %q, want 0 opening %q (stderr %s)", code, out, skips("build"), errOut)
			}
			if code, out, errOut := run("", "doctor", root); code != 0 || !strings.HasPrefix(out, skips("doctor")) {
				t.Fatalf("doctor rc=%d stdout %q, want 0 opening %q (stderr %s)", code, out, skips("doctor"), errOut)
			}
			for _, name := range tc.disabled {
				want := `{"ok":true,"problems":[],"rebuildable":[],"warnings":[],"wrote":0,"unchanged":0,"deleted":0,"error":"","skipped":["` + name + `"]}` + "\n"
				if code, out, errOut := run("", name, "build", root, "--json"); code != 0 || out != want {
					t.Fatalf("%s build --json rc=%d stdout %q, want 0 and %q (stderr %s)", name, code, out, want, errOut)
				}
				want = strings.ToUpper(name) + " CHECK SKIP disabled by ENGINE_SYNC_DISABLE\n"
				if code, out, errOut := run("", name, "check", root); code != 0 || out != want {
					t.Fatalf("%s check rc=%d stdout %q, want 0 and %q (stderr %s)", name, code, out, want, errOut)
				}
			}
			code, out, errOut := run("", "plan", root, "--json")
			var plan struct {
				OK      bool     `json:"ok"`
				Skipped []string `json:"skipped"`
			}
			if err := json.Unmarshal([]byte(out), &plan); err != nil || code != 0 || !plan.OK || !reflect.DeepEqual(plan.Skipped, tc.disabled) {
				t.Fatalf("plan --json rc=%d %q (%v), want ok and skipped %q (stderr %s)", code, out, err, tc.disabled, errOut)
			}
			untouched("every project command")
			for _, name := range tc.enabled {
				if got := mirrorFiles(t, root, name); reflect.DeepEqual(got, before[name]) {
					t.Fatalf("the enabled %s mirror was never rebuilt: %q", name, got)
				}
			}
		})
	}
}

// An unknown name in ENGINE_SYNC_DISABLE refuses every project command.
func TestDisableNamingAnUnknownEngineIsRefused(t *testing.T) {
	root, home := testjail.Scratch(t), testjail.Scratch(t)
	putProjectFile(t, root, "CLAUDE.md", "# Sample\n")
	vars := map[string]string{"HOME": home, "CLAUDE_PROJECT_DIR": root, "ENGINE_SYNC_DISABLE": "codex, claude"}
	e := env{get: func(key string) string { return vars[key] }}
	const refusal = `ENGINE_SYNC_DISABLE: unknown engine "claude"; valid: codex, opencode`
	for _, tc := range []struct {
		args         []string
		out, lastErr string
	}{
		{args: []string{"build", root}, lastErr: "engine-sync build: " + refusal},
		{args: []string{"opencode", "check", root}, lastErr: "engine-sync check: " + refusal},
		{args: []string{"codex", "check", root, "--json"},
			out: `{"ok":false,"problems":[],"rebuildable":[],"warnings":[],"wrote":0,"unchanged":0,"deleted":0,"error":"` + strings.ReplaceAll(refusal, `"`, `\"`) + `"}` + "\n"},
	} {
		var out, errOut bytes.Buffer
		code := dispatch(tc.args, &out, &errOut, e)
		lines := strings.Split(strings.TrimSuffix(errOut.String(), "\n"), "\n")
		if code != 2 || out.String() != tc.out || (tc.lastErr != "" && lines[len(lines)-1] != tc.lastErr) {
			t.Fatalf("%v rc=%d stdout %q stderr %q; want rc=2 stdout %q stderr ending %q", tc.args, code, out.String(), errOut.String(), tc.out, tc.lastErr)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("a refused build wrote AGENTS.md: %v", err)
	}
}

// Machine scope never reads ENGINE_SYNC_DISABLE, a bad value included.
func TestMachineScopeIgnoresDisable(t *testing.T) {
	home := testjail.Scratch(t)
	putProjectFile(t, home, ".claude/agents/reader.md", "---\nname: reader\ndescription: Reads files.\n---\nRead the source.\n")
	vars := map[string]string{"HOME": home}
	e := env{get: func(key string) string { return vars[key] }}
	for _, args := range [][]string{{"global", "--plan"}, {"doctor", "--machine"}} {
		delete(vars, "ENGINE_SYNC_DISABLE")
		var wantOut, wantErr bytes.Buffer
		wantCode := dispatch(args, &wantOut, &wantErr, e)
		if wantOut.Len() == 0 {
			t.Fatalf("%v printed nothing to compare (rc=%d, stderr %s)", args, wantCode, &wantErr)
		}
		for _, value := range []string{"codex,opencode", "bogus"} {
			vars["ENGINE_SYNC_DISABLE"] = value
			var out, errOut bytes.Buffer
			if code := dispatch(args, &out, &errOut, e); code != wantCode || out.String() != wantOut.String() || errOut.String() != wantErr.String() {
				t.Fatalf("%v with ENGINE_SYNC_DISABLE=%q rc=%d\n%s%s\nwant rc=%d\n%s%s", args, value, code, &out, &errOut, wantCode, &wantOut, &wantErr)
			}
		}
	}
}
