package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rezzminator/engine-sync/internal/machine"
	"github.com/rezzminator/engine-sync/internal/testjail"
)

// TestMachineAgentCast drives one source clone through the verbs that render
// machine-global agents: check refuses an unpinned section and global skips
// that variant while the originals install,
// `variants pin` pins it, a requires agent installs only under its
// --capability, doctor reports the closed-world states, OpenCode receives
// the global agents, --adopt moves an unowned file aside (planned under
// --plan, never through a symlinked registry), and --uninstall retires the
// owned OpenCode agents.
func TestMachineAgentCast(t *testing.T) {
	home, _, e := auditEnv(t)
	repo := filepath.Join(home, "clone")
	putGeneratedDir(t, home)
	putProjectFile(t, repo, "CLAUDE.md", "Clone\n")
	putProjectFile(t, repo, "agents/lead.md", "---\nname: lead\ndescription: Leads.\ntools: Read\n---\nLead.\n\n## Budget\n\nSpend little.\n")
	putProjectFile(t, repo, "agents/twin.md", "---\nname: twin\ndescription: Twins.\ntools: Read\nrequires: [gpt-agents]\n---\nTwin.\n")
	putProjectFile(t, repo, "agents/lead-pro.md", "---\nname: lead-pro\nfrom: lead\n---\n\n## Budget\n\nSpend more.\n")
	claudeAgents := filepath.Join(home, ".claude", "agents")
	openCodeAgents := filepath.Join(home, ".config", "opencode", "agents")

	steps := []struct {
		name    string
		args    []string
		plant   map[string]string
		wantRC  int
		want    []string
		absent  []string
		present []string
		lacks   []string
	}{
		{name: "check refuses the unpinned section of the clone's variants", args: []string{"check", repo}, wantRC: 1,
			want: []string{"SECTION-UNPINNED lead-pro: lead.md § Budget has no pin"}},
		{name: "global skips the unpinned variant and installs the originals", args: []string{"global", "--source-repo", repo}, wantRC: 1,
			want:    []string{"SECTION-UNPINNED lead-pro"},
			present: []string{filepath.Join(claudeAgents, "lead.md")}, absent: []string{filepath.Join(claudeAgents, "lead-pro.md")}},
		{name: "variants pin writes the pin", args: []string{"variants", "pin", "--source-repo", repo},
			want: []string{"pinned lead-pro § Budget sha256:"}},
		{name: "doctor before install names MISSING and SKIPPED", args: []string{"doctor", "--machine", "--source-repo", repo}, wantRC: 1,
			want: []string{"agent MISSING claude lead-pro " + filepath.Join(claudeAgents, "lead-pro.md"), "agent SKIPPED all twin ", "(requires gpt-agents unmet)", "agent MISSING opencode lead-pro " + filepath.Join(openCodeAgents, "lead-pro.md")}},
		{name: "an unowned file shadows a declared agent", args: []string{"global", "--source-repo", repo}, wantRC: 1,
			plant: map[string]string{filepath.Join(claudeAgents, "lead.md"): "mine\n", filepath.Join(home, ".codex", "agents", "stranger.toml"): "name = \"stranger\"\n"}},
		{name: "plan with adopt lists the adoption, not a conflict", args: []string{"global", "--plan", "--source-repo", repo, "--adopt"},
			want:  []string{"adopt " + filepath.Join(claudeAgents, "lead.md") + " -> "},
			lacks: []string{"CONFLICT"}},
		{name: "adopt leaves a file under a symlinked registry", args: []string{"global", "--plan", "--source-repo", repo, "--adopt", "--codex-home", filepath.Join(home, "linked-codex")}, wantRC: 1,
			plant: map[string]string{filepath.Join(home, "dotfiles", "agents", "lead.toml"): "name = \"mine\"\n"},
			want:  []string{"not adopted: parent " + filepath.Join(home, "linked-codex", "agents") + " is a symlink"}},
		{name: "adopt moves it aside and installs every engine", args: []string{"global", "--source-repo", repo, "--adopt"},
			want:    []string{"adopted " + filepath.Join(claudeAgents, "lead.md") + " -> "},
			present: []string{filepath.Join(openCodeAgents, "lead.md"), filepath.Join(openCodeAgents, "lead-pro.md"), filepath.Join(home, ".codex", "agents", "lead-pro.toml")},
			absent:  []string{filepath.Join(claudeAgents, "twin.md"), filepath.Join(openCodeAgents, "twin.md")}},
		{name: "the capability installs the requires agent", args: []string{"global", "--source-repo", repo, "--capability", "gpt-agents"},
			present: []string{filepath.Join(claudeAgents, "twin.md"), filepath.Join(openCodeAgents, "twin.md")}},
		{name: "doctor reports INSTALLED and STRAY", args: []string{"doctor", "--machine", "--source-repo", repo, "--capability", "gpt-agents"},
			want: []string{"agent INSTALLED claude twin", "agent INSTALLED opencode lead-pro", "agent INSTALLED codex lead-pro", "agent STRAY codex stranger"}},
	}
	if err := os.MkdirAll(filepath.Join(home, "linked-codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "dotfiles", "agents"), filepath.Join(home, "linked-codex", "agents")); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		for path, content := range step.plant {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				t.Fatal(err) // an installed link is replaced, never written through
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		rc, out := auditRun(e, step.args...)
		if rc != step.wantRC {
			t.Fatalf("%s: %v rc=%d, want %d: %s", step.name, step.args, rc, step.wantRC, out)
		}
		for _, want := range step.want {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: output lacks %q: %s", step.name, want, out)
			}
		}
		for _, lack := range step.lacks {
			if strings.Contains(out, lack) {
				t.Fatalf("%s: output carries %q: %s", step.name, lack, out)
			}
		}
		for _, path := range step.present {
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("%s: %s not written: %v", step.name, path, err)
			}
		}
		for _, path := range step.absent {
			if _, err := os.Lstat(path); err == nil {
				t.Fatalf("%s: %s present, want it absent", step.name, path)
			}
		}
	}
	adopted, err := filepath.Glob(filepath.Join(home, ".local", "state", "engine-sync", "adopted", "*", ".claude", "agents", "lead.md"))
	if err != nil || len(adopted) != 1 {
		t.Fatalf("adopted files = %v (%v), want one", adopted, err)
	}
	if raw, err := os.ReadFile(adopted[0]); err != nil || string(raw) != "mine\n" {
		t.Fatalf("adopted content = %q (%v), want the unowned file", raw, err)
	}
	if target, err := os.Readlink(filepath.Join(claudeAgents, "lead.md")); err != nil || target != filepath.Join(repo, "agents", "lead.md") {
		t.Fatalf("lead link = %q (%v), want the clone source", target, err)
	}
	if raw, err := os.ReadFile(filepath.Join(repo, "agents", "lead-pro.md")); err != nil || !strings.Contains(string(raw), "pins:") {
		t.Fatalf("declaration not pinned: %s (%v)", raw, err)
	}
	mine := filepath.Join(openCodeAgents, "mine.md")
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, out := auditRun(e, "global", "--uninstall", "--source-repo", repo); rc != 0 {
		t.Fatalf("uninstall rc=%d: %s", rc, out)
	}
	for _, name := range []string{"lead.md", "lead-pro.md", "twin.md"} {
		if _, err := os.Lstat(filepath.Join(openCodeAgents, name)); err == nil {
			t.Fatalf("uninstall left the owned OpenCode agent %s", name)
		}
	}
	if _, err := os.Lstat(mine); err != nil {
		t.Fatalf("uninstall removed the unowned OpenCode agent: %v", err)
	}
}

// One machine-global agent whose frontmatter does not parse is skipped and
// named FRONTMATTER-INVALID: global fails closed on it, and doctor still
// reports every other agent's row.
func TestMachineBrokenSourceIsSkippedAndNamed(t *testing.T) {
	home, _, e := auditEnv(t)
	repo := filepath.Join(home, "clone")
	putProjectFile(t, repo, "CLAUDE.md", "Clone\n")
	putProjectFile(t, repo, "agents/lead.md", "---\nname: lead\ndescription: Leads.\ntools: Read\n---\nLead.\n")
	putProjectFile(t, repo, "agents/bad.md", "---\nname: bad\n\tdescription: Bad.\n---\nBad.\n")
	named := "FRONTMATTER-INVALID " + filepath.Join(repo, "agents", "bad.md") + ":3"
	for _, args := range [][]string{{"doctor", "--machine", "--source-repo", repo}, {"global", "--source-repo", repo}} {
		rc, out := auditRun(e, args...)
		if rc != 1 || !strings.Contains(out, named) {
			t.Fatalf("%v rc=%d, want 1 naming the broken source: %s", args, rc, out)
		}
		if args[0] == "doctor" && (!strings.Contains(out, "agent MISSING claude lead ") || strings.Contains(out, " bad "+filepath.Join(home, ".config"))) {
			t.Fatalf("doctor lacks the valid agent's row: %s", out)
		}
	}
}

// An agent skipped for an error never blocks the rest: a broken source and a
// variant whose render fails are each named, the healthy agents install, the
// run exits non-zero, and the copies an earlier install left stay on disk.
// A malformed declaration skips only itself while healthy declarations install.
func TestMachineErrorSkipsKeepTheInstalledCopies(t *testing.T) {
	home, _, e := auditEnv(t)
	repo := filepath.Join(home, "clone")
	putGeneratedDir(t, home)
	putProjectFile(t, repo, "CLAUDE.md", "Clone\n")
	putProjectFile(t, repo, "agents/lead.md", "---\nname: lead\ndescription: Leads.\ntools: Read\n---\nLead.\n")
	putProjectFile(t, repo, "agents/bad.md", "---\nname: bad\ndescription: Bad.\ntools: Read\n---\nBad.\n")
	for _, name := range []string{"good", "broken"} {
		putProjectFile(t, repo, "agents/"+name+".md", "---\nname: "+name+"\nfrom: lead\n---\n")
	}
	if rc, out := auditRun(e, "global", "--source-repo", repo); rc != 0 {
		t.Fatalf("first install rc=%d: %s", rc, out)
	}
	generated := filepath.Join(repo, "generated", "agents")
	installed := func(names ...string) []string {
		var out []string
		for _, name := range names {
			out = append(out, filepath.Join(home, ".claude", "agents", name+".md"), filepath.Join(home, ".codex", "agents", name+".toml"), filepath.Join(home, ".config", "opencode", "agents", name+".md"))
		}
		return out
	}
	putProjectFile(t, repo, "agents/bad.md", "---\nname: bad\n\tdescription: Bad.\n---\nBad.\n")
	putProjectFile(t, repo, "agents/broken.md", "---\nname: broken\nfrom: lead\nunset: [nokey]\n---\n")
	putProjectFile(t, repo, "agents/fresh.md", "---\nname: fresh\ndescription: Fresh.\ntools: Read\n---\nFresh.\n")
	rc, out := auditRun(e, "global", "--source-repo", repo)
	if rc == 0 || !strings.Contains(out, "FRONTMATTER-INVALID "+filepath.Join(repo, "agents", "bad.md")+":3") || !strings.Contains(out, "UNSET-MISSING broken") {
		t.Fatalf("global rc=%d, want non-zero naming the broken source and the broken variant: %s", rc, out)
	}
	for _, path := range append(installed("lead", "good", "fresh", "bad", "broken"), filepath.Join(generated, "broken.md")) {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("after the error skips, %s: %v\n%s", path, err, out)
		}
	}
	if rc, out := auditRun(e, "doctor", "--machine", "--source-repo", repo); rc == 0 || !strings.Contains(out, "UNSET-MISSING broken") || strings.Contains(out, "agent STALE codex broken") {
		t.Fatalf("doctor rc=%d, want the broken variant failing and its kept role never STALE: %s", rc, out)
	}
	// A source that parses but breaks the agent contract (a retired flat key)
	// is skipped the same way, its copies kept; --adopt never turns one
	// broken agent into a failure of the whole install.
	putProjectFile(t, repo, "agents/lead.md", "---\nname: lead\ndescription: Leads.\ntools: Read\ncodex-model: x\n---\nLead.\n")
	putProjectFile(t, repo, "agents/newer.md", "---\nname: newer\ndescription: Newer.\ntools: Read\n---\nNewer.\n")
	rc, out = auditRun(e, "global", "--source-repo", repo, "--adopt")
	if rc == 0 || !strings.Contains(out, "codex-model moved to engines.codex.model") {
		t.Fatalf("global --adopt rc=%d, want non-zero naming the retired key: %s", rc, out)
	}
	for _, path := range installed("lead", "newer", "fresh") {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("after the contract skip, %s: %v\n%s", path, err, out)
		}
	}
	putProjectFile(t, repo, "agents/lead.md", "---\nname: lead\ndescription: Leads.\ntools: Read\n---\nLead.\n")
	putProjectFile(t, repo, "agents/broken.md", "---\nname: broken\nfrom: lead\nengines:\n  codex: [unclosed\n---\n")
	putProjectFile(t, repo, "agents/later.md", "---\nname: later\ndescription: Later.\ntools: Read\n---\nLater.\n")
	rc, out = auditRun(e, "global", "--source-repo", repo)
	if rc == 0 || !strings.Contains(out, "FRONTMATTER-INVALID "+filepath.Join(repo, "agents", "broken.md")) {
		t.Fatalf("global rc=%d, want non-zero naming the declaration as the one error: %s", rc, out)
	}
	for _, path := range append(installed("later", "good", "broken"), filepath.Join(generated, "good.md")) {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("after the declaration failed, %s: %v\n%s", path, err, out)
		}
	}
}

// TestAdoptMovesAFileListedUnderTwoSpellingsOnce: one Codex home given as
// itself and through a link to it lists the same unowned role twice; adopt
// moves it aside once (the linked spelling stays a named CONFLICT of the
// build, so the run exits 1).
func TestAdoptMovesAFileListedUnderTwoSpellingsOnce(t *testing.T) {
	home, _, e := auditEnv(t)
	repo := filepath.Join(home, "clone")
	putProjectFile(t, repo, "CLAUDE.md", "Clone\n")
	putProjectFile(t, repo, "agents/lead.md", "---\nname: lead\ndescription: Leads.\ntools: Read\n---\nLead.\n")
	outside := testjail.Scratch(t)
	real, link := filepath.Join(outside, "codex"), filepath.Join(outside, "codex-link")
	putProjectFile(t, real, "agents/lead.toml", "name = \"mine\"\n")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	rc, out := auditRun(e, "global", "--source-repo", repo, "--adopt", "--codex-home", real, "--codex-home", link)
	if rc != 1 || strings.Count(out, "adopted ") != 1 || strings.Contains(out, "to adopt:") {
		t.Fatalf("adopt rc=%d, want one adoption: %s", rc, out)
	}
	var adopted []string
	err := filepath.WalkDir(filepath.Join(home, ".local", "state", "engine-sync", "adopted"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			adopted = append(adopted, path)
		}
		return err
	})
	if err != nil || len(adopted) != 1 || filepath.Base(adopted[0]) != "lead.toml" {
		t.Fatalf("adopted files = %v (%v), want one lead.toml", adopted, err)
	}
	// A Codex home outside home that is itself a symlink is a tree the build
	// refuses to write: its hand file is never adopted.
	putProjectFile(t, real, "agents/lead.toml", "name = \"mine\"\n")
	rc, out = auditRun(e, "global", "--source-repo", repo, "--adopt", "--codex-home", link)
	if rc != 1 || strings.Contains(out, "adopted ") || !strings.Contains(out, "not adopted: parent "+link+" is a symlink") {
		t.Fatalf("adopt through a linked Codex home rc=%d, want no adoption: %s", rc, out)
	}
	if raw, err := os.ReadFile(filepath.Join(real, "agents", "lead.toml")); err != nil || string(raw) != "name = \"mine\"\n" {
		t.Fatalf("hand role behind the link = %q (%v), want it in place", raw, err)
	}
}

// TestVariantsPinWaitsForTheWriterGuard: a pin rewrites the declaration under
// its agents directory's writer guard, so while another writer holds it the
// pin waits, and two pins never both pass the re-read before either renames.
func TestVariantsPinWaitsForTheWriterGuard(t *testing.T) {
	home, _, e := auditEnv(t)
	repo := filepath.Join(home, "clone")
	putGeneratedDir(t, home)
	putProjectFile(t, repo, "agents/lead.md", "---\nname: lead\ndescription: Leads.\ntools: Read\n---\nLead.\n")
	putProjectFile(t, repo, "agents/lead-pro.md", "---\nname: lead-pro\nfrom: lead\n---\n")
	done := make(chan int, 1)
	early := false
	_, err := guarded(e, []string{machine.GlobalRoot(filepath.Join(repo, "agents"))}, true, func() int {
		go func() {
			rc, _ := auditRun(e, "variants", "pin", "--source-repo", repo)
			done <- rc
		}()
		select {
		case rc := <-done:
			early = true
			t.Errorf("pin finished rc=%d while another writer held the guard, want it waiting", rc)
		case <-time.After(300 * time.Millisecond):
		}
		return 0
	})
	if err != nil {
		t.Fatalf("hold the writer guard: %v", err)
	}
	if !early {
		if rc := <-done; rc != 0 {
			t.Fatalf("pin after the guard was released rc=%d, want 0", rc)
		}
	}
}

// Claude loads registry files raw; neither adoption nor a directory alias may
// turn machine source controls into a live agent or overwrite their bytes.
func TestMachineRefusesRawDeclarationsInClaudeRegistry(t *testing.T) {
	for _, form := range []string{"default", "physical alias", "second registry"} {
		t.Run(form, func(t *testing.T) {
			home, _, e := auditEnv(t)
			store := filepath.Join(home, ".claude")
			var args []string
			if form == "second registry" {
				store = filepath.Join(home, "second")
			}
			dir := filepath.Join(store, "agents")
			plain := "---\nname: reader\ndescription: Read.\n---\nRead.\n"
			files := map[string]string{"reader.md": plain, "derived.md": "---\nname: derived\nfrom: general-purpose\n---\nDerived.\n", "layer.md": "---\nname: layer\nabstract: true\nmodel: sonnet\n---\n"}
			for name, raw := range files {
				putProjectFile(t, store, "agents/"+name, raw)
			}
			if form == "physical alias" {
				repo := filepath.Join(home, "clone")
				putProjectFile(t, repo, "CLAUDE.md", "Clone\n")
				if err := os.Symlink(dir, filepath.Join(repo, "agents")); err != nil {
					t.Fatal(err)
				}
				args = []string{"--source-repo", repo}
			}
			if form == "second registry" {
				raw := `{"version":1,"machine":{"claudeAgentsDir":"` + dir + `"},"targets":{"claudeConfigDirs":["` + filepath.Join(home, ".claude") + `","` + store + `"]}}`
				putProjectFile(t, home, ".claude/engine-sync.json", raw)
			}
			for _, command := range [][]string{append([]string{"global", "--adopt"}, args...), append([]string{"doctor", "--machine"}, args...)} {
				out := auditBad(t, e, "Claude Code loads it raw", command...)
				for _, text := range []string{"derived.md", "layer.md", "sourceAgentsDir", "sourceRepo", "outside every Claude agents dir"} {
					if !strings.Contains(out, text) {
						t.Fatalf("%v output lacks %q: %s", command, text, out)
					}
				}
				for name, want := range files {
					got, err := os.ReadFile(filepath.Join(dir, name))
					if err != nil || string(got) != want {
						t.Fatalf("source %s changed: %q (%v)", name, got, err)
					}
				}
			}
			got, err := os.ReadFile(filepath.Join(home, ".codex", "agents", "reader.toml"))
			if err != nil || !strings.Contains(string(got), `name = "reader"`) {
				t.Fatalf("healthy plain agent not installed: %q (%v)", got, err)
			}
		})
	}
}

// A declaration renders only into machine.sourceGeneratedAgentsDir: with the
// key unset it is skipped by name, the original still installs, and the run
// exits 1.
func TestDeclarationWithoutAGeneratedDirIsSkippedByName(t *testing.T) {
	home, _, e := auditEnv(t)
	repo := filepath.Join(home, "clone")
	putProjectFile(t, repo, "CLAUDE.md", "Clone\n")
	putProjectFile(t, repo, "agents/astra.md", "---\nname: astra\ndescription: Astra.\ntools: Read\n---\nAstra.\n")
	putProjectFile(t, repo, "agents/astra-pro.md", "---\nname: astra-pro\nfrom: astra\neffort: high\n---\n")
	rc, out := auditRun(e, "global", "--source-repo", repo)
	if rc != 1 || !strings.Contains(out, "VARIANT-INVALID astra-pro") || !strings.Contains(out, "machine.sourceGeneratedAgentsDir") {
		t.Fatalf("global rc=%d, want 1 naming VARIANT-INVALID astra-pro and machine.sourceGeneratedAgentsDir: %s", rc, out)
	}
	for _, path := range []string{filepath.Join(home, ".claude", "agents", "astra.md"), filepath.Join(home, ".codex", "agents", "astra.toml")} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("the original did not install: %v", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "agents", "astra-pro.md")); !os.IsNotExist(err) {
		t.Errorf("the skipped declaration installed: %v", err)
	}
}
