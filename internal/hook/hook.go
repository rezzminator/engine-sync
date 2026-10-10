package hook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/atomicfile"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/paths"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type StageRunner func([]string) (int, string)
type Payload struct {
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
	StopHookActive bool `json:"stop_hook_active"`
}
type Context struct {
	Get     paths.Env
	Home    func() (string, error)
	State   func(string) string
	Policy  func(string, string, io.Writer) (*config.Config, error)
	Targets func(string) (bool, error)
	Writer  func() string
}

const detailLimit = 4000

// ReadPayload decodes the hook payload; a missing or malformed one reads as
// empty, which every hook treats as nothing to do.
func ReadPayload(stdin io.Reader) Payload {
	var payload Payload
	if stdin == nil {
		return payload
	}
	raw, err := io.ReadAll(stdin)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return payload
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Payload{}
	}
	return payload
}

// IsClaudeSource reports whether a written file is one the mirrors compile
// from: the home's global commands, anything under a .claude directory, a
// CLAUDE.md or a .mcp.json.
func IsClaudeSource(path, home string) bool {
	if home != "" && strings.HasPrefix(path, filepath.Join(home, ".claude", "commands")+string(filepath.Separator)) {
		return true
	}
	slash := filepath.ToSlash(path)
	return strings.Contains(slash, "/.claude/") || strings.HasSuffix(slash, "/CLAUDE.md") ||
		strings.HasSuffix(slash, "/.mcp.json")
}

// ProjectRoot is $CLAUDE_PROJECT_DIR, else dir's git superproject, else its
// git toplevel: the harness project root, whose .claude/engine-sync.json
// governs, never a nested submodule's.
func ProjectRoot(e Context, dir string) string {
	if root := e.Get("CLAUDE_PROJECT_DIR"); root != "" {
		return root
	}
	for _, flag := range []string{"--show-superproject-working-tree", "--show-toplevel"} {
		out, err := paths.Git("-C", dir, "rev-parse", flag)
		if root := strings.TrimSpace(string(out)); err == nil && root != "" {
			return root
		}
	}
	return ""
}

// InScope reports whether a project opted in: it carries the compiler's
// configuration, or an output already holds a generated marker.
func InScope(root string) (bool, error) {
	for _, rel := range []string{config.RelPath, config.LegacyRelPath} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			return true, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("inspect %s: %w", root, err)
		}
	}
	for _, path := range []string{filepath.Join(root, "AGENTS.md"), filepath.Join(root, ".opencode", "opencode.jsonc")} {
		raw, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("read %s: %w", path, err)
		}
		if artifact.Claims(string(raw)) {
			return true, nil
		}
	}
	return false, nil
}

// DirtyFlag is the project's flag under {engine-sync state}/dirty: the
// directory name (leading dot stripped) for a reader, a hash of the root so
// two projects of one name never share it.
func DirtyFlag(e Context, root string) (string, error) {
	home, err := e.Home()
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	sum := sha256.Sum256([]byte(root))
	name := strings.TrimPrefix(filepath.Base(root), ".") + "-" + hex.EncodeToString(sum[:])[:16]
	return filepath.Join(e.State(home), "dirty", name), nil
}

func Mark(payload Payload, stderr io.Writer, e Context) int {
	path := payload.ToolInput.FilePath
	if path == "" {
		return 0
	}
	home, _ := e.Home()
	if !IsClaudeSource(path, home) {
		return 0
	}
	root := ProjectRoot(e, filepath.Dir(path))
	if root == "" {
		return 0
	}
	if _, err := e.Policy(root, home, stderr); err != nil {
		fmt.Fprintln(stderr, err)
		return 0
	}
	if scoped, err := InScope(root); err != nil || !scoped {
		if err != nil {
			fmt.Fprintf(stderr, "engine-sync hook claude mark: %v\n", err)
		}
		return 0
	}
	flag, err := DirtyFlag(e, root)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(flag), 0o700)
	}
	if err == nil {
		err = atomicfile.Write(flag, []byte(root+"\n"), 0o600)
	}
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync hook claude mark: set the dirty flag: %v\n", err)
	}
	return 0
}

// stopStages are the four stages a Stop runs, in order, each named as the
// block reason names it.
func stopStages(root string) [][]string {
	return [][]string{
		{"codex", "build", root}, {"codex", "check", root},
		{"opencode", "build", root}, {"opencode", "check", root},
	}
}

func Stop(payload Payload, stdout, stderr io.Writer, e Context, runner StageRunner) int {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	root := ProjectRoot(e, cwd)
	if root == "" {
		return 0
	}
	flag, err := DirtyFlag(e, root)
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync hook claude stop: %v\n", err)
		return 0
	}
	if _, err := os.Stat(flag); err != nil {
		return 0
	}
	// Claim the flag before the stages read the sources: a mark that lands
	// while they run sets a fresh flag, which this pass never clears.
	claim := fmt.Sprintf("%s.stop-%d", flag, os.Getpid())
	if err := atomicfile.Move(flag, claim); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(stderr, "engine-sync hook claude stop: claim the dirty flag: %v\n", err)
		}
		return 0
	}
	var failed []string
	var detail strings.Builder
	for _, stage := range stopStages(root) {
		code, output := runner(stage)
		if code == 0 {
			continue
		}
		name := stage[0] + " " + stage[1]
		failed = append(failed, name)
		detail.WriteString(name + ":\n" + output + "\n")
	}
	if len(failed) == 0 {
		if err := os.Remove(claim); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(stderr, "engine-sync hook claude stop: clear the dirty flag: %v\n", err)
		}
		return 0
	}
	if err := restoreFlag(claim, flag); err != nil {
		fmt.Fprintf(stderr, "engine-sync hook claude stop: keep the dirty flag: %v\n", err)
	}
	stages := strings.Join(failed, ", ") + " (binary: " + e.Writer() + ")"
	text := detail.String()
	if len(text) > detailLimit {
		text = text[:detailLimit]
	}
	var answer any
	if payload.StopHookActive {
		answer = map[string]string{"systemMessage": "engine-sync WARNING: " + stages +
			" still failing after the repair turn — the mirror is stale; the flag stays set and the check repeats next turn."}
	} else {
		answer = map[string]string{"decision": "block", "reason": "engine-sync: " + stages +
			" failed after this turn's framework edits — the mirror is stale. Repair it now, then end the turn: " +
			"a dangling ~/.claude link → engine-sync global (prunes orphaned global-command links; a stale " +
			"~/.claude/agents link is removed by hand); a stale or orphan mirror file → engine-sync build .; " +
			"then engine-sync check . must pass. Output of the failing stage(s):\n" + text}
	}
	if err := json.NewEncoder(stdout).Encode(answer); err != nil {
		fmt.Fprintf(stderr, "engine-sync hook claude stop: write the answer: %v\n", err)
	}
	return 0
}

// restoreFlag puts a claimed flag back after a failed pass; a fresh flag a
// concurrent mark set already keeps the project dirty.
func restoreFlag(claim, flag string) error {
	if _, err := os.Lstat(flag); err == nil {
		return os.Remove(claim)
	}
	return atomicfile.Move(claim, flag)
}

// Start rebuilds a stale mirror (a `git pull` lands here) and refreshes
// the machine targets when targets.json names them. A build that fails sets
// the dirty flag, so the session's first Stop reports it.
func Start(stderr io.Writer, e Context, runner StageRunner) int {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	if root := ProjectRoot(e, cwd); root != "" {
		if scoped, err := InScope(root); err != nil {
			fmt.Fprintf(stderr, "engine-sync hook claude start: %v\n", err)
		} else if scoped {
			if code, _ := runner([]string{"check", root}); code != 0 {
				if code, output := runner([]string{"build", root}); code != 0 {
					fmt.Fprintf(stderr, "engine-sync hook claude start: build %s failed:\n%s", root, output)
					if flag, err := DirtyFlag(e, root); err == nil && os.MkdirAll(filepath.Dir(flag), 0o700) == nil {
						if err := atomicfile.Write(flag, []byte(root+"\n"), 0o600); err != nil {
							fmt.Fprintf(stderr, "engine-sync hook claude start: set the dirty flag: %v\n", err)
						}
					}
				}
			}
		}
	}
	home, err := e.Home()
	if err != nil {
		return 0
	}
	if found, err := e.Targets(home); err != nil {
		fmt.Fprintf(stderr, "engine-sync hook claude start: %v\n", err)
	} else if found {
		if code, output := runner([]string{"global"}); code != 0 {
			fmt.Fprintf(stderr, "engine-sync hook claude start: global failed:\n%s", output)
		}
	}
	return 0
}
