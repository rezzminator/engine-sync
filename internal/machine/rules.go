package machine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/atomicfile"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	reconciler "github.com/rezzminator/engine-sync/internal/reconcile"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"go.yaml.in/yaml/v3"
)

// globalRulesBudget is the always-on set's size past which doctor warns: every
// agent of every project pays it.
const globalRulesBudget = 8 << 10

// openCodeInstructionsKey is the global OpenCode config's array of extra
// instruction files.
const openCodeInstructionsKey = "instructions"

// globalRules is the always-on set of one Claude rules directory, compiled.
type globalRules struct {
	Dir   string
	Label string // the directory as the compiled text names it
	// Body is every always-on rule, transformed, each under its
	// `<!-- rule: … -->` line, in sorted relative-path order; "" when none.
	Body   string
	Scoped []string // path-scoped rules: delivered by no channel yet
	Broken []string // RULE-FRONTMATTER and RULE-FENCE rows: each rule skipped
	// Warnings name the command links the /command roster skipped.
	Warnings []string
}

func openCodeConfigPath(home string) string {
	return filepath.Join(home, ".config", "opencode", "opencode.jsonc")
}

func compiledRulesFile(home string) string {
	return filepath.Join(paths.EngineSyncHome(paths.Getenv, home), "rules", "global-always-on.md")
}

func instructionReceiptPath(home string) string {
	return filepath.Join(paths.EngineSyncState(paths.Getenv, home), "opencode-instructions.json")
}

// loadGlobalRules reads every rule under {claudeDir}/rules. An absent
// directory holds no rules; one that cannot be read, or a rule that cannot,
// is an error, never an empty set.
func loadGlobalRules(home, claudeDir string, policy config.Config) (globalRules, error) {
	set := globalRules{Dir: filepath.Join(claudeDir, "rules")}
	set.Label = set.Dir
	if rel, err := filepath.Rel(home, set.Dir); err == nil && !strings.HasPrefix(rel, "..") {
		set.Label = "~/" + filepath.ToSlash(rel)
	}
	rels, err := ruleFiles(set.Dir)
	if err != nil {
		return globalRules{}, err
	}
	transform, problems, warnings := codex.RuleTransform(filepath.Join(claudeDir, "commands"), policy)
	if len(problems) != 0 {
		return globalRules{}, fmt.Errorf("read the /command roster for rules: %s", strings.Join(problems, "; "))
	}
	set.Warnings = warnings
	var blocks []string
	for _, rel := range rels {
		path := filepath.Join(set.Dir, rel)
		raw, err := paths.ReadRegular(path)
		if err != nil {
			return globalRules{}, fmt.Errorf("read rule %s: %w", path, err)
		}
		front, body, err := source.Parse(string(raw))
		if err != nil {
			set.Broken = append(set.Broken, fmt.Sprintf("RULE-FRONTMATTER %s: %v", path, err))
			continue
		}
		if pathScoped(source.MapValue(front.Node, "paths")) {
			set.Scoped = append(set.Scoped, path)
			continue
		}
		compiled := strings.TrimSpace(transform(body, path))
		if strings.Contains(compiled, artifact.RulesBegin) || strings.Contains(compiled, artifact.RulesEnd) {
			// Compiled, it would leave two fences in AGENTS.md, which every
			// later run then refuses.
			set.Broken = append(set.Broken, fmt.Sprintf("RULE-FENCE %s: the body holds the engine-sync rules fence line", path))
			continue
		}
		blocks = append(blocks, "<!-- rule: "+set.Label+"/"+filepath.ToSlash(rel)+" -->\n"+compiled+"\n")
	}
	set.Body = strings.Join(blocks, "\n")
	return set, nil
}

// pathScoped reports a `paths:` value naming at least one path.
func pathScoped(value *yaml.Node) bool {
	return value != nil && value.Tag != "!!null" && (len(value.Content) != 0 || strings.TrimSpace(value.Value) != "")
}

// ruleFiles lists every .md below dir, following links, as sorted relative
// paths; an absent dir has none. A dangling link that is not a .md names no
// rule and is passed over; one that is, or any other failure, is an error.
func ruleFiles(dir string) ([]string, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect rules %s: %w", dir, err)
	}
	var rels []string
	visited := map[string]bool{}
	var walk func(current, relative string) error
	walk = func(current, relative string) error {
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil {
			return fmt.Errorf("resolve rules %s: %w", current, err)
		}
		if visited[resolved] {
			return nil // a cycle: the directory is already being listed
		}
		visited[resolved] = true
		defer delete(visited, resolved)
		entries, err := os.ReadDir(current)
		if err != nil {
			return fmt.Errorf("read rules %s: %w", current, err)
		}
		for _, entry := range entries {
			path, rel := filepath.Join(current, entry.Name()), filepath.Join(relative, entry.Name())
			isRule := strings.HasSuffix(entry.Name(), ".md")
			if !isRule && !entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
				continue
			}
			info, err := os.Stat(path)
			if !isRule && errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("inspect rule %s: %w", path, err)
			}
			if info.IsDir() {
				if err := walk(path, rel); err != nil {
					return err
				}
			} else if isRule {
				rels = append(rels, rel)
			}
		}
		return nil
	}
	if err := walk(dir, ""); err != nil {
		return nil, err
	}
	sort.Slice(rels, func(i, j int) bool { return filepath.ToSlash(rels[i]) < filepath.ToSlash(rels[j]) })
	return rels, nil
}

func (r *run) rulesClaudeDir() string {
	return r.claudeConfigDirs()[0]
}

// globalRules compiles the always-on global rules into every Codex home's
// AGENTS.md block and the OpenCode instructions entry; uninstall removes
// both and reads no rule.
func (r *run) globalRules() error {
	if r.options.skipRules {
		return nil
	}
	var set globalRules
	if !r.options.Uninstall {
		var err error
		if set, err = loadGlobalRules(r.options.Home, r.rulesClaudeDir(), r.policy); err != nil {
			return fmt.Errorf("global rules: %w", err)
		}
		for _, broken := range set.Broken {
			r.skip(broken + " — rule skipped")
		}
		for _, warning := range set.Warnings {
			r.skip("global rules /command roster: " + warning)
		}
	}
	return errors.Join(r.codexRules(set), r.openCodeRules(set))
}

func rulesBlock(set globalRules) string {
	if set.Body == "" {
		return ""
	}
	return artifact.RulesBegin + "\n" + set.Body + artifact.RulesEnd + "\n"
}

func (r *run) codexRules(set globalRules) error {
	block := rulesBlock(set)
	var failed []error
	for _, home := range r.codexHomes() {
		path := filepath.Join(home, "AGENTS.md")
		content, mode, err := readOwnedFile(path)
		if err != nil {
			failed = append(failed, err)
			continue
		}
		next, err := spliceRulesBlock(content, block)
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", path, err))
			continue
		}
		if next == content {
			continue
		}
		if next == "" {
			r.add(VerbChange, "delete "+path+" (global rules block, all it held)", "")
			if !r.options.Plan {
				if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
					failed = append(failed, fmt.Errorf("remove %s: %w", path, err))
				}
			}
			continue
		}
		r.add(VerbChange, "write "+path+" (global rules block)", "")
		if !r.options.Plan {
			if err := atomicfile.Write(path, []byte(next), mode); err != nil {
				failed = append(failed, err)
			}
		}
	}
	return errors.Join(failed...)
}

// readOwnedFile reads a file engine-sync edits a part of: absent is empty
// with mode 0644; a link or special file is refused, never replaced.
func readOwnedFile(path string) (string, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", 0o644, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("%s is not a regular file; preserved", path)
	}
	raw, err := paths.ReadRegular(path)
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", path, err)
	}
	return string(raw), info.Mode().Perm(), nil
}

// spliceRulesBlock replaces the owned block of content with block, appends it
// when content has none, and removes it when block is empty. The append puts
// one "\n" before the block: a blank line after a file ending in a newline,
// the line end of one that does not. Removing a block that ends the file
// takes that one "\n" back, so append then remove returns the original bytes;
// a block with text after it is cut out alone. Text outside the fence is kept
// byte for byte; a fence that is not exactly one BEGIN before one END is
// refused.
func spliceRulesBlock(content, block string) (string, error) {
	begins, ends := strings.Count(content, artifact.RulesBegin), strings.Count(content, artifact.RulesEnd)
	if begins == 0 && ends == 0 {
		switch {
		case block == "":
			return content, nil
		case content == "":
			return block, nil
		}
		return content + "\n" + block, nil
	}
	start, stop := strings.Index(content, artifact.RulesBegin), strings.Index(content, artifact.RulesEnd)
	if begins != 1 || ends != 1 || stop < start {
		return "", fmt.Errorf("the engine-sync rules fence is not one BEGIN line before one END line; preserved")
	}
	end := stop + len(artifact.RulesEnd)
	switch {
	case strings.HasPrefix(content[end:], "\r\n"):
		end += 2
	case strings.HasPrefix(content[end:], "\n"):
		end++
	}
	before := content[:start]
	if block == "" && end == len(content) && strings.HasSuffix(before, "\n") {
		before = before[:len(before)-1]
	}
	return before + block + content[end:], nil
}

// rulesRows are doctor's global-rules rows: the source set, each Codex home's
// block and the OpenCode file and entry. A rules directory that cannot be
// read is one ERROR row and nothing is compared, never "no rules".
func rulesRows(options DoctorOptions) []Row {
	cfg := options.Config
	if cfg == nil {
		resolved, err := config.LoadConfigLayers("", options.Home)
		if err != nil {
			return []Row{{Level: LevelFail, Message: "global-rules NOT CHECKED — the config did not load: " + err.Error()}}
		}
		cfg = &resolved
	}
	claudeDir := cfg.ClaudeConfigDir
	if len(options.ClaudeConfigDirs) != 0 {
		claudeDir = options.ClaudeConfigDirs[0]
	} else if options.ClaudeConfigDir != "" {
		claudeDir = options.ClaudeConfigDir
	}
	set, err := loadGlobalRules(options.Home, claudeDir, *cfg)
	if err != nil {
		return []Row{{Level: LevelFail, Message: "global-rules ERROR " + err.Error() + " — no rule compiled or checked"}}
	}
	rows := []Row{{Level: LevelOK, Message: fmt.Sprintf("global-rules source=%s always-on bytes=%d", set.Dir, len(set.Body))}}
	for _, broken := range set.Broken {
		rows = append(rows, Row{Level: LevelFail, Message: broken})
	}
	for _, warning := range set.Warnings {
		rows = append(rows, Row{Level: LevelWarn, Message: "global-rules /command roster: " + warning})
	}
	for _, scoped := range set.Scoped {
		rows = append(rows, Row{Level: LevelWarn, Message: "global-rules PATH-SCOPED " + scoped + ` not delivered to Codex or OpenCode note="the path-scoped injector is not built"`})
	}
	if len(set.Body) > globalRulesBudget {
		rows = append(rows, Row{Level: LevelWarn, Message: fmt.Sprintf("GLOBAL-BUDGET %d > %d bytes of always-on global rules, paid by every agent of every project", len(set.Body), globalRulesBudget)})
	}
	codexHomes := options.CodexHomes
	if codexHomes == nil {
		codexHomes = []string{filepath.Join(options.Home, ".codex")}
	}
	block := rulesBlock(set)
	for _, home := range codexHomes {
		rows = append(rows, codexRulesRow(home, block))
	}
	return append(rows, openCodeRulesRows(options.Home, set)...)
}

func rulesRow(state reconciler.State, subject string) Row {
	if state == "ok" {
		return Row{Level: LevelOK, Message: "global-rules " + subject + " state=ok"}
	}
	return Row{Level: LevelFail, Message: "global-rules " + subject + " state=" + string(state) + ` hint="run engine-sync global"`}
}

func codexRulesRow(home, block string) Row {
	path := filepath.Join(home, "AGENTS.md")
	subject := "codex " + path
	content, _, err := readOwnedFile(path)
	if err != nil {
		return Row{Level: LevelFail, Message: "global-rules ERROR " + subject + ": " + err.Error()}
	}
	next, err := spliceRulesBlock(content, block)
	if err != nil {
		return Row{Level: LevelFail, Message: "global-rules ERROR " + subject + ": " + err.Error()}
	}
	if block != "" {
		override := filepath.Join(home, "AGENTS.override.md")
		if _, err := os.Lstat(override); err == nil {
			return Row{Level: LevelFail, Message: "global-rules " + subject + " state=SHADOWED" + ` note="Codex reads ` + override + ` instead"`}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Row{Level: LevelFail, Message: "global-rules ERROR " + subject + ": inspect " + override + ": " + err.Error()}
		}
	}
	switch {
	case next == content:
		return rulesRow("ok", subject)
	case block != "" && !strings.Contains(content, artifact.RulesBegin):
		return rulesRow(reconciler.Missing, subject)
	default:
		return rulesRow(reconciler.Stale, subject)
	}
}

func openCodeRulesRows(home string, set globalRules) []Row {
	compiled := compiledRulesFile(home)
	want := compiledRules(set)
	var rows []Row
	current, _, err := readOwnedFile(compiled)
	switch {
	case err != nil:
		rows = append(rows, Row{Level: LevelFail, Message: "global-rules ERROR opencode " + err.Error()})
	case current == want:
		rows = append(rows, rulesRow("ok", "opencode "+compiled))
	case current == "":
		rows = append(rows, rulesRow(reconciler.Missing, "opencode "+compiled))
	default:
		rows = append(rows, rulesRow(reconciler.Stale, "opencode "+compiled))
	}
	plan, err := planInstructions(home, compiled, want != "")
	subject := "opencode instructions entry " + plan.Config
	switch {
	case err != nil:
		rows = append(rows, Row{Level: LevelFail, Message: "global-rules ERROR " + subject + ": " + err.Error()})
	case slices.Equal(plan.Entries, plan.Next):
		rows = append(rows, rulesRow("ok", subject))
	case want != "" && !slices.Contains(plan.Entries, compiled):
		rows = append(rows, rulesRow(reconciler.Missing, subject))
	default:
		rows = append(rows, rulesRow(reconciler.Stale, subject))
	}
	return rows
}
