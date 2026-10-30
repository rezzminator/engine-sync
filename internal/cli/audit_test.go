package cli

import (
	"bytes"
	"encoding/json"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/guard"
	"github.com/rezzminator/engine-sync/internal/hook"
	"github.com/rezzminator/engine-sync/internal/pluginlog"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const auditAgent = "---\nname: reader\ndescription: Reads.\ntools: Read\n---\nRead.\n"

func auditEnv(t *testing.T) (string, map[string]string, env) {
	t.Helper()
	home := testjail.Scratch(t)
	vars := map[string]string{"HOME": home}
	return home, vars, env{get: func(k string) string { return vars[k] }}
}
func auditRun(e env, args ...string) (int, string) {
	var out, errout bytes.Buffer
	rc := dispatch(args, &out, &errout, e)
	return rc, out.String() + errout.String()
}
func auditOK(t *testing.T, e env, args ...string) string {
	t.Helper()
	rc, out := auditRun(e, args...)
	if rc != 0 {
		t.Fatalf("%v rc=%d %s", args, rc, out)
	}
	return out
}
func auditBad(t *testing.T, e env, contains string, args ...string) string {
	t.Helper()
	rc, out := auditRun(e, args...)
	if rc != 1 || !strings.Contains(out, contains) {
		t.Fatalf("%v rc=%d want error %q: %s", args, rc, contains, out)
	}
	return out
}

func TestAuditCWDDiscovery(t *testing.T) {
	for _, form := range []string{"subdirectory config", "machine marker", "home git"} {
		t.Run(form, func(t *testing.T) {
			home, vars, e := auditEnv(t)
			root := filepath.Join(home, "code", "sample")
			putProjectFile(t, root, "CLAUDE.md", "Sample\n")
			if form == "subdirectory config" {
				putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1,"codex":{"suffixPrefix":"audit-prefix"}}`)
			}
			if form == "machine marker" {
				putProjectFile(t, home, ".claude/engine-sync.json", `{"version":1,"workbenchMarker":"bench.json"}`)
				putProjectFile(t, root, "bench.json", `{}`)
				// The marker, rather than orientation fallback, must identify the root.
				if err := os.Remove(filepath.Join(root, "CLAUDE.md")); err != nil {
					t.Fatal(err)
				}
				putProjectFile(t, home, "code/CLAUDE.md", "Parent\n")
			}
			if form == "home git" {
				putProjectFile(t, home, ".git/HEAD", "ref: refs/heads/main\n")
				putProjectFile(t, home, "CLAUDE.md", "Home\n")
			}
			sub := filepath.Join(root, "sub")
			if err := os.MkdirAll(sub, 0755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(sub)
			out := auditOK(t, e, "config", "validate")
			if !strings.Contains(out, "VALID "+root) {
				t.Fatalf("wrong root: %s", out)
			}
			if form == "subdirectory config" {
				out = auditOK(t, e, "config", "show")
				if !strings.Contains(out, "audit-prefix") {
					t.Fatal(out)
				}
			}
			putProjectFile(t, root, "CLAUDE.md", "Sample\n")
			auditOK(t, e, "build")
			if form != "machine marker" {
				if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(filepath.Join(home, "AGENTS.md")); !os.IsNotExist(err) {
				t.Fatalf("home selected: %v", err)
			}
			if form == "machine marker" {
				putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1}`)
				putProjectFile(t, root, "CLAUDE.md", "Changed\n")
				vars["CLAUDE_PROJECT_DIR"] = ""
				e.stdin = strings.NewReader(`{"tool_input":{"file_path":` + strconvQuote(filepath.Join(root, "CLAUDE.md")) + `}}`)
				auditOK(t, e, "hook", "claude", "mark")
				flag, err := hook.DirtyFlag(hookContext(e), root)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = os.Stat(flag); err != nil {
					t.Fatal(err)
				}
				e.stdin = strings.NewReader(`{}`)
				auditOK(t, e, "hook", "claude", "stop")
				if _, err = os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAuditMachineFailures(t *testing.T) {
	for _, form := range []string{"unwritable global", "unwritable agents", "foreign role", "foreign plan", "bare old copy", "dangling source", "locked foreign"} {
		t.Run(form, func(t *testing.T) {
			// An unwritable agents directory is a permission bit, which root ignores.
			if strings.HasPrefix(form, "unwritable") && testjail.RanUnprivileged(t) {
				return
			}
			home, _, e := auditEnv(t)
			repo := filepath.Join(home, "source")
			putProjectFile(t, repo, "agents/reader.md", auditAgent)
			args := []string{"global", "--source-repo", repo}
			if form == "unwritable agents" || form == "foreign role" {
				args = []string{"codex", "agents", "--source-repo", repo}
			}
			if strings.HasPrefix(form, "unwritable") {
				// The role plans as missing, then its write fails: the failure under test is the write's.
				dir := filepath.Join(home, ".codex", "agents")
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
				if err := os.WriteFile(filepath.Join(dir, "probe"), nil, 0600); err == nil {
					t.Fatal("fixture does not deny the write")
				}
			}
			if form == "foreign role" || form == "foreign plan" {
				putProjectFile(t, home, ".codex/agents/reader.toml", "user role\n")
			}
			if form == "foreign plan" {
				args = append(args, "--plan")
			}
			if form == "bare old copy" {
				putProjectFile(t, home, ".claude/agents/reader.md", "stale copy from the old installer\n")
			}
			if form == "dangling source" {
				// Default source registry, as left by a removed checkout.
				putProjectFile(t, home, ".claude/agents/reader.md", auditAgent)
				if err := os.Symlink(filepath.Join(home, "missing"), filepath.Join(home, ".claude/agents/gone.md")); err != nil {
					t.Fatal(err)
				}
				args = []string{"global"}
			}
			if form == "locked foreign" {
				// A symlink loop where a role file belongs: reading it fails with ELOOP for every uid.
				p := filepath.Join(home, ".codex", "agents", "locked.toml")
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p, p); err != nil {
					t.Fatal(err)
				}
				if _, err := os.ReadFile(p); err == nil {
					t.Fatal("fixture does not deny the read")
				}
			}
			rc, out := auditRun(e, args...)
			if form == "dangling source" || form == "locked foreign" {
				// A bad entry is named, while good independent roles still compile.
				if !strings.Contains(out, map[string]string{"dangling source": "gone.md", "locked foreign": "locked.toml"}[form]) {
					t.Errorf("missing diagnostic: %s", out)
				}
				if _, err := os.Stat(filepath.Join(home, ".codex", "agents", "reader.toml")); err != nil {
					t.Errorf("good role lost: %v rc=%d %s", err, rc, out)
				}
			} else if rc != 1 || strings.Contains(out, "CODEX AGENTS PASS") {
				t.Errorf("false success: rc=%d %s", rc, out)
			}
			if strings.HasPrefix(form, "unwritable") && !strings.Contains(out, "write "+filepath.Join(home, ".codex", "agents", "reader.toml")+": ") {
				t.Errorf("the failed write is not named: %s", out)
			}
			if form == "foreign role" && !strings.Contains(out, "compiled") {
				t.Errorf("unlabelled compiled size: %s", out)
			}
			for _, line := range strings.Split(out, "\n") {
				line = strings.Join(strings.Fields(line), " ")
				if strings.HasPrefix(line, "change write ") && form != "foreign plan" {
					name := strings.TrimPrefix(line, "change write ")
					if _, err := os.Stat(name); err != nil {
						t.Errorf("write reported before execution: %s", line)
					}
				}
			}
			if form == "bare old copy" {
				raw, err := os.ReadFile(filepath.Join(home, ".claude", "agents", "reader.md"))
				if err != nil || string(raw) != "stale copy from the old installer\n" {
					t.Errorf("foreign copy changed: %s %v", raw, err)
				}
			}
			if form == "foreign plan" && strings.Count(out, "CONFLICT "+filepath.Join(home, ".codex", "agents", "reader.toml")) != 1 {
				t.Errorf("conflict duplicated: %s", out)
			}
		})
	}
}

func TestAuditRosterAndTargets(t *testing.T) {
	for _, form := range []string{"empty homes", "bad targets explicit", "bad targets disabled", "source and registries", "uninstall", "empty source"} {
		t.Run(form, func(t *testing.T) {
			home, _, e := auditEnv(t)
			repo := filepath.Join(home, "repo")
			putProjectFile(t, repo, "agents/reader.md", auditAgent)
			args := []string{"global", "--source-repo", repo}
			switch form {
			case "empty homes":
				putTargets(t, home, `{"codexHomes":[]}`)
			case "bad targets explicit", "bad targets disabled":
				putTargets(t, home, `{bad`)
				if form == "bad targets explicit" {
					args = append(args, "--codex-home", filepath.Join(home, "chosen"))
				} else {
					args = append(args, "--no-codex-home")
				}
			case "source and registries", "uninstall":
				raw, _ := json.Marshal(map[string]any{"codexHomes": []string{filepath.Join(home, ".codex")}, "sourceRepo": repo, "claudeConfigDirs": []string{filepath.Join(home, ".claude"), filepath.Join(home, "other")}})
				putTargets(t, home, string(raw))
				putProjectFile(t, home, ".claude/agents/foreign.md", strings.ReplaceAll(auditAgent, "reader", "foreign"))
				args = []string{"global"}
			case "empty source":
				if err := os.Remove(filepath.Join(repo, "agents", "reader.md")); err != nil {
					t.Fatal(err)
				}
			}
			if form == "bad targets explicit" || form == "bad targets disabled" {
				// The capability set reads the global file's targets: a broken one fails
				// the run, never an empty capability set.
				auditBad(t, e, "engine-sync.json", args...)
				return
			}
			auditOK(t, e, args...)
			if form == "empty homes" {
				auditOK(t, e, "doctor", "--machine", "--source-repo", repo)
				auditOK(t, e, "codex", "agents", "--source-repo", repo)
				if _, err := os.Stat(filepath.Join(home, ".codex", "agents", "reader.toml")); !os.IsNotExist(err) {
					t.Errorf("empty roster wrote role: %v", err)
				}
			}
			if form == "source and registries" {
				auditOK(t, e, "doctor", "--machine")
				if _, err := os.Stat(filepath.Join(home, ".codex", "agents", "foreign.toml")); !os.IsNotExist(err) {
					t.Errorf("wrong source: %v", err)
				}
			}
			if form == "uninstall" {
				auditOK(t, e, "global", "--uninstall")
				for _, dir := range []string{".claude", "other"} {
					if _, err := os.Lstat(filepath.Join(home, dir, "agents", "reader.md")); !os.IsNotExist(err) {
						t.Errorf("dangling registry link %s: %v", dir, err)
					}
				}
			}
		})
	}
}

func TestAuditHookFailurePaths(t *testing.T) {
	for _, form := range []string{"invalid project mark", "invalid machine unrelated", "start machine independent", "busy project"} {
		t.Run(form, func(t *testing.T) {
			home, vars, e := auditEnv(t)
			root := filepath.Join(home, "repo")
			vars["CLAUDE_PROJECT_DIR"] = root
			putProjectFile(t, root, "CLAUDE.md", "Sample\n")
			putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1}`)
			if form == "invalid project mark" || form == "start machine independent" {
				putProjectFile(t, root, ".claude/engine-sync.json", `{bad`)
			}
			if form == "invalid machine unrelated" {
				putProjectFile(t, home, ".claude/engine-sync.json", `{bad`)
				e.stdin = strings.NewReader(`{"tool_input":{"file_path":"/tmp/unrelated.txt"}}`)
				if out := auditOK(t, e, "hook", "claude", "mark"); out != "" {
					t.Fatal(out)
				}
				return
			}
			if form == "start machine independent" {
				putProjectFile(t, home, ".claude/agents/reader.md", auditAgent)
				e.stdin = strings.NewReader(`{}`)
				auditOK(t, e, "hook", "claude", "start")
				if _, err := os.Stat(filepath.Join(home, ".codex", "agents", "reader.toml")); err != nil {
					t.Fatal(err)
				}
				return
			}
			e.stdin = strings.NewReader(`{"tool_input":{"file_path":` + strconvQuote(filepath.Join(root, "CLAUDE.md")) + `}}`)
			auditOK(t, e, "hook", "claude", "mark")
			flag, err := hook.DirtyFlag(hookContext(e), root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(flag); err != nil {
				t.Fatal(err)
			}
			if form == "busy project" {
				release, err := guard.Lock(e.state(home), root)
				if err != nil {
					t.Fatal(err)
				}
				timer := time.AfterFunc(3*time.Second, func() { _ = release() })
				defer func() {
					if timer.Stop() {
						_ = release()
					}
				}()
			}
			before := time.Now()
			e.stdin = strings.NewReader(`{}`)
			out := auditOK(t, e, "hook", "claude", "stop")
			if form == "busy project" {
				// A live holder is no fault: no screen line, an info row, the flag kept (checked below).
				if time.Since(before) > 2*time.Second || out != "" {
					t.Errorf("busy lock answered late or on screen: %q", out)
				}
				logs := pluginlog.Dir("", home, "engine-sync")
				info, err := os.ReadFile(filepath.Join(logs, "info.jsonl"))
				if err != nil || !strings.Contains(string(info), `"event":"lock-busy"`) || !strings.Contains(string(info), "holder pid=") {
					t.Errorf("no lock-busy info row: %v %s", err, info)
				}
				if errs, err := os.ReadFile(filepath.Join(logs, "errors.jsonl")); err == nil && strings.Contains(string(errs), "BUSY") {
					t.Errorf("a held lock wrote an error row: %s", errs)
				}
			} else if !strings.Contains(out, `"decision":"block"`) {
				t.Error(out)
			}
			if _, err = os.Stat(flag); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAuditUnusedAndForeignCommands(t *testing.T) {
	for _, form := range []string{"global disabled", "workbench", "extra directory", "foreign home command", "external home skill", "env home", "foreign home skill"} {
		t.Run(form, func(t *testing.T) {
			home, vars, e := auditEnv(t)
			root := testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "Sample\n")
			if form == "global disabled" {
				putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1,"codex":{"globalCommands":false}}`)
			}
			if form == "workbench" {
				putProjectFile(t, root, ".claude/engine-sync.json", `{"version":1,"workbenchMarker":"bench.json"}`)
				putProjectFile(t, root, "bench.json", `{}`)
			}
			if form == "global disabled" || form == "workbench" {
				// A regular file where the commands directory belongs: listing it fails with ENOTDIR for every uid.
				p := filepath.Join(home, ".claude", "commands")
				putProjectFile(t, home, ".claude/commands", "secret\n")
				if _, err := os.ReadDir(p); err == nil {
					t.Fatal("fixture does not deny the listing")
				}
			}
			if form == "extra directory" {
				putProjectFile(t, root, filepath.Join("templates", "global", "commands", "unused.md"), "---\ndescription: Not a live command.\n---\nUnused\n")
			}
			if form == "foreign home command" {
				putProjectFile(t, home, ".config/opencode/command/hand.md", "handwritten, no frontmatter\n")
			}
			if form == "external home skill" || form == "env home" {
				chosen := testjail.Scratch(t)
				vars["CODEX_HOME"] = chosen
				putProjectFile(t, home, ".claude/commands/hello/SKILL.md", "---\nname: hello\ndescription: Hello.\n---\nHello\n")
			}
			if form == "foreign home skill" {
				putProjectFile(t, home, ".claude/skills/hand/SKILL.md", "Hand\n")
				if err := os.MkdirAll(filepath.Join(home, ".codex", "skills"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(home, ".claude", "skills", "hand"), filepath.Join(home, ".codex", "skills", "hand")); err != nil {
					t.Fatal(err)
				}
			}
			auditOK(t, e, "build", root)
			auditOK(t, e, "check", root)
			if form == "external home skill" || form == "env home" {
				p := filepath.Join(vars["CODEX_HOME"], "skills", "hello")
				target, err := os.Readlink(p)
				if err != nil || target != filepath.Join(home, ".claude", "commands", "hello") {
					t.Errorf("wrong link %s %v", target, err)
				}
				if _, err = os.Stat(p); err != nil {
					t.Fatal(err)
				}
			}
			if form == "extra directory" {
				for _, rel := range []string{".codex/skills/unused", ".opencode/command/unused.md"} {
					if _, err := os.Lstat(filepath.Join(root, rel)); !os.IsNotExist(err) {
						t.Errorf("unused templates compiled: %s %v", rel, err)
					}
				}
			}
			if form == "foreign home skill" {
				if _, err := os.Readlink(filepath.Join(home, ".codex", "skills", "hand")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAuditHomeCommandOlderWriter(t *testing.T) {
	home, _, e := auditEnv(t)
	first, second := testjail.Scratch(t), testjail.Scratch(t)
	for _, root := range []string{first, second} {
		putProjectFile(t, root, "CLAUDE.md", "Sample\n")
	}
	putProjectFile(t, home, ".claude/commands/hello.md", "---\ndescription: Hello.\n---\nHello\n")
	auditOK(t, e, "opencode", "build", first)
	if err := guard.Check(e.state(home), "global:"+filepath.Join(home, ".config", "opencode", "command"), "0.1.0"); err == nil {
		t.Fatal("home command dir has no writer record")
	}
	saved := Version
	Version = "0.1.0"
	defer func() { Version = saved }()
	auditBad(t, e, "NEWER-WRITER", "opencode", "build", second)
}

func TestAuditDoctorBrokenState(t *testing.T) {
	for _, form := range []string{"tampered generated agent", "tampered role", "orphan role", "stale prompt", "deleted skill", "unreadable prompts", "fifo skill", "corrupt writer", "bad targets", "second registry"} {
		t.Run(form, func(t *testing.T) {
			// Unreadable prompts are a permission bit, which root ignores.
			if form == "unreadable prompts" && testjail.RanUnprivileged(t) {
				return
			}
			home, _, e := auditEnv(t)
			if form != "tampered generated agent" {
				putProjectFile(t, home, ".claude/agents/reader.md", auditAgent)
			}
			putProjectFile(t, home, ".claude/commands/hello.md", "---\ndescription: Hello.\n---\nHello\n")
			putProjectFile(t, home, ".claude/commands/family/SKILL.md", "---\nname: family\ndescription: Family.\n---\nFamily\n")
			if form == "second registry" {
				raw, _ := json.Marshal(map[string]any{"codexHomes": []string{filepath.Join(home, ".codex")}, "sourceRepo": filepath.Join(home, ".claude"), "claudeConfigDirs": []string{filepath.Join(home, ".claude"), filepath.Join(home, "other")}})
				putTargets(t, home, string(raw))
			}
			if form == "tampered generated agent" {
				putProjectFile(t, home, "agent-sources/reader.md", auditAgent)
				putProjectFile(t, home, "agent-sources/reader-pro.md", "---\nname: reader-pro\nfrom: reader\ndescription: Deep read.\n---\n")
				raw, err := json.Marshal(map[string]any{"version": 1, "machine": map[string]any{"claudeAgentsDir": filepath.Join(home, "agent-sources"), "sourceGeneratedAgentsDir": "generated/agents"}})
				if err != nil {
					t.Fatal(err)
				}
				putProjectFile(t, home, ".claude/engine-sync.json", string(raw))
			}
			auditOK(t, e, "global")
			switch form {
			case "tampered generated agent":
				putProjectFile(t, home, ".claude/generated/agents/reader-pro.md", artifact.Marker(artifact.Markdown, "agents/reader.md")+"\nchanged\n")
			case "tampered role":
				putProjectFile(t, home, ".codex/agents/reader.toml", artifact.Marker(artifact.TOML, "agents/reader.md")+"\nname = \"changed\"\n")
			case "orphan role":
				putProjectFile(t, home, ".codex/agents/gone.toml", artifact.Marker(artifact.TOML, "agents/gone.md")+"\n")
			case "stale prompt":
				putProjectFile(t, home, ".codex/prompts/hello.md", artifact.Marker(artifact.Markdown, "commands/hello.md")+"\nchanged\n")
			case "deleted skill":
				if err := os.Remove(filepath.Join(home, ".codex", "skills", "family")); err != nil {
					t.Fatal(err)
				}
			case "unreadable prompts":
				p := filepath.Join(home, ".codex", "prompts")
				if err := os.Chmod(p, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(p, 0755) })
				if _, err := os.ReadDir(p); err == nil {
					t.Fatal("fixture does not deny the listing")
				}
			case "fifo skill":
				p := filepath.Join(home, ".codex", "skills", "extra", "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(p, 0600); err != nil {
					t.Fatal(err)
				}
			case "corrupt writer":
				files, err := filepath.Glob(filepath.Join(e.state(home), "writers", "*.json"))
				if err != nil || len(files) == 0 {
					t.Fatalf("writer files %v %v", files, err)
				}
				if err := os.WriteFile(files[0], []byte(`{bad`), 0600); err != nil {
					t.Fatal(err)
				}
			case "bad targets":
				putTargets(t, home, `{bad`)
			case "second registry":
				if err := os.Remove(filepath.Join(home, "other", "agents", "reader.md")); err != nil {
					t.Fatal(err)
				}
			}
			rc, out := auditRun(e, "doctor", "--machine")
			if rc != 1 {
				t.Fatalf("doctor false ok rc=%d %s", rc, out)
			}
			if form == "unreadable prompts" && !strings.Contains(out, "inspect "+filepath.Join(home, ".codex", "prompts")+":") {
				t.Errorf("the unlistable prompts directory is not named: %s", out)
			}
			if strings.Contains(out, "ok global-agents") {
				t.Errorf("contradictory registry ok: %s", out)
			}
		})
	}
}

func TestAuditProjectCollisionsAndNames(t *testing.T) {
	for _, names := range [][2]string{{"Reader-Api", "reader"}, {"caf\u00e9-api", "cafe\u0301"}} {
		t.Run(names[0], func(t *testing.T) {
			_, _, e := auditEnv(t)
			root := testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "Root\n")
			putProjectFile(t, root, "api/CLAUDE.md", "Child\n")
			// The role name is the frontmatter name: each file declares its own base.
			putProjectFile(t, root, ".claude/agents/"+names[0]+".md", strings.ReplaceAll(auditAgent, "name: reader", "name: "+names[0]))
			putProjectFile(t, root, "api/.claude/agents/"+names[1]+".md", strings.NewReplacer("tools: Read", "tools: Write", "name: reader", "name: "+names[1]).Replace(auditAgent))
			out := auditBad(t, e, "CONFLICT", "codex", "build", root)
			for _, name := range names {
				if !strings.Contains(out, name+".md") {
					t.Error(out)
				}
			}
		})
	}
	for _, name := range []string{"../../ESCAPED", "sub/dir"} {
		t.Run(name, func(t *testing.T) {
			home, _, e := auditEnv(t)
			putProjectFile(t, home, ".claude/agents/reader.md", strings.ReplaceAll(auditAgent, "name: reader", "name: "+name))
			auditBad(t, e, "name", "global")
			if _, err := os.Stat(filepath.Join(home, "ESCAPED.toml")); !os.IsNotExist(err) {
				t.Fatalf("escaped %v", err)
			}
		})
	}
}

func TestAuditSourceAndJSONCFailures(t *testing.T) {
	for _, form := range []string{"device source", "unparseable jsonc", "compiled forms", "empty description", "invalid yaml"} {
		t.Run(form, func(t *testing.T) {
			_, _, e := auditEnv(t)
			root := testjail.Scratch(t)
			putProjectFile(t, root, "CLAUDE.md", "Sample\n")
			putProjectFile(t, root, ".claude/agents/reader.md", auditAgent)
			switch form {
			case "device source":
				if err := os.Symlink("/dev/zero", filepath.Join(root, ".claude", "agents", "zero.md")); err != nil {
					t.Fatal(err)
				}
			case "unparseable jsonc":
				putProjectFile(t, root, ".opencode/opencode.jsonc", artifact.Marker(artifact.JSONC, "configuration + .mcp.json")+"\n{ invalid jsonc")
			case "compiled forms":
				putProjectFile(t, root, ".claude/agents/reader.md", "\ufeff"+strings.ReplaceAll(strings.ReplaceAll(auditAgent, "---\n", "--- \n"), "\n", "\r\n"))
			case "empty description":
				putProjectFile(t, root, ".claude/agents/reader.md", strings.ReplaceAll(auditAgent, "description: Reads.", "description:"))
			}
			if form == "invalid yaml" {
				// One YAML parser serves agents and commands, and both engines
				// name its error the same way, line included.
				broken := "---\nname: broken\n\tdescription: Reads.\n---\nBody.\n"
				putProjectFile(t, root, ".claude/agents/broken.md", broken)
				putProjectFile(t, root, ".claude/commands/broken.md", broken)
				rc, out := auditRun(e, "check", root)
				for _, file := range []string{".claude/agents/broken.md", ".claude/commands/broken.md"} {
					named := "FRONTMATTER-INVALID " + filepath.Join(root, filepath.FromSlash(file)) + ":3 — "
					if rc == 0 || strings.Count(out, named) != 2 {
						t.Fatalf("check rc=%d, want %q once per engine:\n%s", rc, named, out)
					}
				}
				return
			}
			if form == "compiled forms" {
				auditOK(t, e, "codex", "build", root)
				raw, err := os.ReadFile(filepath.Join(root, ".codex", "agents", "reader.toml"))
				if err != nil || !strings.Contains(string(raw), `sandbox_mode = "read-only"`) {
					t.Fatalf("sandbox lost %s %v", raw, err)
				}
			} else {
				auditBad(t, e, map[string]string{"device source": "regular", "unparseable jsonc": "UNPARSEABLE", "empty description": "description"}[form], "build", root)
			}
			if form == "unparseable jsonc" {
				raw, err := os.ReadFile(filepath.Join(root, ".opencode", "opencode.jsonc"))
				if err != nil || string(raw) != artifact.Marker(artifact.JSONC, "configuration + .mcp.json")+"\n{ invalid jsonc" {
					t.Fatalf("keeper changed %s %v", raw, err)
				}
			}
		})
	}
}

func TestAuditPopulatedHomesAndProjectRosters(t *testing.T) {
	home, _, e := auditEnv(t)
	second := testjail.Scratch(t)
	putProjectFile(t, home, ".claude/commands/global.md", "---\ndescription: Global.\n---\nUse /foo:bar.\n")
	auditOK(t, e, "global")
	raw, _ := json.Marshal(map[string]any{"codexHomes": []string{filepath.Join(home, ".codex"), second}})
	putTargets(t, home, string(raw))
	auditOK(t, e, "global")
	for _, h := range []string{filepath.Join(home, ".codex"), second} {
		if _, err := os.Stat(filepath.Join(h, "prompts", "global.md")); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(home, ".config", "opencode", "command", "global.md")
	before, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"agent", "command"} {
		root := filepath.Join(testjail.Scratch(t), prefix, "project")
		putProjectFile(t, root, "CLAUDE.md", "Use /foo:bar.\n")
		putProjectFile(t, root, ".claude/agents/reader.md", auditAgent)
		if prefix == "agent" {
			putProjectFile(t, root, ".claude/commands/foo/bar.md", "---\ndescription: Local.\n---\nLocal\n")
		}
		auditOK(t, e, "build", root)
		after, err := os.ReadFile(dest)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("project roster changed home: %s %v", after, err)
		}
	}
}
