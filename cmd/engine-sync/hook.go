package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rezzminator/engine-sync/internal/codexgen"
	"github.com/rezzminator/engine-sync/internal/opencodegen"
)

// stageRunner runs one engine-sync command and returns its exit code and
// combined output; the hooks run their stages in-process through run().
type stageRunner func(args []string) (int, string)

// hookPayload is the part of a Claude hook payload the hooks read.
type hookPayload struct {
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
	StopHookActive bool `json:"stop_hook_active"`
}

// detailLimit bounds the failing stages' output carried into a block reason.
const detailLimit = 4000

func runHook(args []string, stdout, stderr io.Writer, e env) int {
	if len(args) != 2 || args[0] != "claude" {
		if len(args) >= 1 && (args[0] == "codex" || args[0] == "opencode") {
			// Path-scoped rule injection lands in P3 (docs/design rules): no
			// hook entry calls it yet, and a call answers nothing.
			return 0
		}
		fmt.Fprintln(stderr, "usage: engine-sync hook claude mark|stop|start")
		return 2
	}
	payload := readPayload(e.stdin)
	runner := func(stage []string) (int, string) {
		var out bytes.Buffer
		code := run(stage, &out, &out, e)
		return code, out.String()
	}
	switch args[1] {
	case "mark":
		return hookMark(payload, stderr, e)
	case "stop":
		return hookStop(payload, stdout, stderr, e, runner)
	case "start":
		return hookStart(stderr, e, runner)
	}
	fmt.Fprintln(stderr, "usage: engine-sync hook claude mark|stop|start")
	return 2
}

// readPayload decodes the hook payload; a missing or malformed one reads as
// empty, which every hook treats as nothing to do.
func readPayload(stdin io.Reader) hookPayload {
	var payload hookPayload
	if stdin == nil {
		return payload
	}
	raw, err := io.ReadAll(stdin)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return payload
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return hookPayload{}
	}
	return payload
}

// isClaudeSource reports whether a written file is one the mirrors compile
// from: the home's global commands, anything under a .claude directory, a
// CLAUDE.md or a .mcp.json.
func isClaudeSource(path, home string) bool {
	if home != "" && strings.HasPrefix(path, filepath.Join(home, ".claude", "commands")+string(filepath.Separator)) {
		return true
	}
	slash := filepath.ToSlash(path)
	return strings.Contains(slash, "/.claude/") || strings.HasSuffix(slash, "/CLAUDE.md") ||
		strings.HasSuffix(slash, "/.mcp.json")
}

// projectRoot is $CLAUDE_PROJECT_DIR, else dir's git superproject, else its
// git toplevel: the harness project root, whose .claude/codex-build.json
// governs, never a nested submodule's.
func projectRoot(e env, dir string) string {
	if root := e.get("CLAUDE_PROJECT_DIR"); root != "" {
		return root
	}
	for _, flag := range []string{"--show-superproject-working-tree", "--show-toplevel"} {
		out, err := exec.Command("git", "-C", dir, "rev-parse", flag).Output()
		if root := strings.TrimSpace(string(out)); err == nil && root != "" {
			return root
		}
	}
	return ""
}

// inScope reports whether a project opted in: it carries the compiler's
// configuration, or an output already holds a generated marker.
func inScope(root string) (bool, error) {
	if _, err := os.Stat(filepath.Join(root, ".claude", "codex-build.json")); err == nil {
		return true, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("inspect %s: %w", root, err)
	}
	for path, markers := range map[string][]string{
		filepath.Join(root, "AGENTS.md"):                   codexgen.GeneratedMarkers(),
		filepath.Join(root, ".opencode", "opencode.jsonc"): opencodegen.GeneratedMarkers(),
	} {
		raw, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("read %s: %w", path, err)
		}
		for _, marker := range markers {
			if bytes.Contains(raw, []byte(marker)) {
				return true, nil
			}
		}
	}
	return false, nil
}

// dirtyFlag is the project's flag under {engine-sync state}/dirty: the
// directory name (leading dot stripped) for a reader, a hash of the root so
// two projects of one name never share it.
func dirtyFlag(e env, root string) (string, error) {
	home, err := e.home()
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	sum := sha256.Sum256([]byte(root))
	name := strings.TrimPrefix(filepath.Base(root), ".") + "-" + hex.EncodeToString(sum[:])[:16]
	return filepath.Join(e.state(home), "dirty", name), nil
}

func hookMark(payload hookPayload, stderr io.Writer, e env) int {
	path := payload.ToolInput.FilePath
	if path == "" {
		return 0
	}
	home, _ := e.home()
	if !isClaudeSource(path, home) {
		return 0
	}
	root := projectRoot(e, filepath.Dir(path))
	if root == "" {
		return 0
	}
	if scoped, err := inScope(root); err != nil || !scoped {
		if err != nil {
			fmt.Fprintf(stderr, "engine-sync hook claude mark: %v\n", err)
		}
		return 0
	}
	flag, err := dirtyFlag(e, root)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(flag), 0o700)
	}
	if err == nil {
		err = os.WriteFile(flag, []byte(root+"\n"), 0o600)
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

func hookStop(payload hookPayload, stdout, stderr io.Writer, e env, runner stageRunner) int {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	root := projectRoot(e, cwd)
	if root == "" {
		return 0
	}
	flag, err := dirtyFlag(e, root)
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
	if err := os.Rename(flag, claim); err != nil {
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
	stages := strings.Join(failed, ", ") + " (binary: " + writerPath() + ")"
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
			"a dangling ~/.claude link → pfm install --yes (prunes orphaned global-command links; a stale " +
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
	return os.Rename(claim, flag)
}

// hookStart rebuilds a stale mirror (a `git pull` lands here) and refreshes
// the machine targets when targets.json names them. A build that fails sets
// the dirty flag, so the session's first Stop reports it.
func hookStart(stderr io.Writer, e env, runner stageRunner) int {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	if root := projectRoot(e, cwd); root != "" {
		if scoped, err := inScope(root); err != nil {
			fmt.Fprintf(stderr, "engine-sync hook claude start: %v\n", err)
		} else if scoped {
			if code, _ := runner([]string{"check", root}); code != 0 {
				if code, output := runner([]string{"build", root}); code != 0 {
					fmt.Fprintf(stderr, "engine-sync hook claude start: build %s failed:\n%s", root, output)
					if flag, err := dirtyFlag(e, root); err == nil && os.MkdirAll(filepath.Dir(flag), 0o700) == nil {
						if err := os.WriteFile(flag, []byte(root+"\n"), 0o600); err != nil {
							fmt.Fprintf(stderr, "engine-sync hook claude start: set the dirty flag: %v\n", err)
						}
					}
				}
			}
		}
	}
	home, err := e.home()
	if err != nil {
		return 0
	}
	if _, found, err := readTargets(e, home); err != nil {
		fmt.Fprintf(stderr, "engine-sync hook claude start: %v\n", err)
	} else if found {
		if code, output := runner([]string{"global"}); code != 0 {
			fmt.Fprintf(stderr, "engine-sync hook claude start: global failed:\n%s", output)
		}
	}
	return 0
}
