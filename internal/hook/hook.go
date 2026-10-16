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
	"github.com/rezzminator/engine-sync/internal/guard"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/pluginlog"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type StageRunner func([]string) (int, string)
type Payload struct {
	SessionID string `json:"session_id"`
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
	StopHookActive bool `json:"stop_hook_active"`
}
type Context struct {
	SessionID string `json:"session_id"`
	Get       paths.Env
	Home      func() (string, error)
	State     func(string) string
	Policy    func(string, string, io.Writer) (*config.Config, error)
	Targets   func(string) (bool, error)
	Writer    func() string
	Engines   []string
}

const detailLimit = 4000

// ReadPayload decodes the hook payload; a missing or malformed one reads as
// empty, which every hook treats as nothing to do.
func ReadPayload(stdin io.Reader) Payload {
	var payload Payload
	if stdin == nil {
		return payload
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
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

func resolveProjectRoot(e Context, dir string, session bool) (string, error) {
	home, err := e.Home()
	if err != nil {
		return "", err
	}
	explicit := e.Get("CLAUDE_PROJECT_DIR")
	if explicit != "" {
		root, err := paths.ProjectRoot(explicit, home)
		if err != nil {
			return "", err
		}
		physical, err := paths.PhysicalPath(dir)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, physical)
		if err != nil {
			return "", err
		}
		if session || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) || strings.HasPrefix(physical, filepath.Join(home, ".claude", "commands")+string(filepath.Separator)) {
			return root, nil
		}
	}
	marker := ""
	policy, policyErr := e.Policy("", home, io.Discard)
	if policyErr == nil {
		marker = policy.WorkbenchMarker
	}
	root, err := paths.DiscoverRoot(dir, home, marker)
	if err != nil && policyErr != nil {
		return "", errors.Join(policyErr, err)
	}
	return root, err
}
func hookNotice(stdout, stderr io.Writer, message string) {
	fmt.Fprintln(stderr, message)
	if stdout != nil {
		if err := json.NewEncoder(stdout).Encode(map[string]string{"systemMessage": message}); err != nil {
			fmt.Fprintln(stderr, err)
		}
	}
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
		raw, err := paths.ReadHeader(path)
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
	root, err = paths.PhysicalPath(root)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(root))
	name := strings.TrimPrefix(filepath.Base(root), ".") + "-" + hex.EncodeToString(sum[:])[:16]
	return filepath.Join(e.State(home), "dirty", name), nil
}

func Mark(payload Payload, stderr io.Writer, e Context) int {
	if payload.SessionID != "" {
		e.SessionID = payload.SessionID
	}
	path := payload.ToolInput.FilePath
	if path == "" {
		return 0
	}
	home, homeErr := e.Home()
	if homeErr != nil {
		recordHook(e, stderr, "hook-step-failed", "Mark", homeErr.Error(), nil)
	}
	if !IsClaudeSource(path, home) {
		return 0
	}
	root, rootErr := resolveProjectRoot(e, filepath.Dir(path), false)
	if rootErr != nil {
		recordHook(e, stderr, "hook-step-failed", "Mark", rootErr.Error(), nil)
		fmt.Fprintf(stderr, "engine-sync hook claude mark: %v\n", rootErr)
		return 0
	}
	if root == "" {
		return 0
	}
	if scoped, err := InScope(root); err != nil || !scoped {
		if err != nil {
			recordHook(e, stderr, "hook-step-failed", "Mark", err.Error(), nil)
			fmt.Fprintf(stderr, "engine-sync hook claude mark: %v\n", err)
		}
		return 0
	}
	flag, err := DirtyFlag(e, root)
	var release func() error
	if err == nil {
		release, err = guard.Lock(e.State(home), "dirty:"+root, 250*time.Millisecond)
	}
	if release != nil {
		defer func() {
			if err := release(); err != nil {
				recordHook(e, stderr, "hook-lock-failed", "Mark", "release dirty lock: "+err.Error(), nil)
				fmt.Fprintln(stderr, "engine-sync hook claude mark: release dirty lock:", err)
			}
		}()
	}
	if err == nil {
		err = os.MkdirAll(filepath.Dir(flag), 0o700)
	}
	if err == nil {
		err = atomicfile.Write(flag, []byte(root+"\n"), 0o600)
	}
	if err != nil {
		recordHook(e, stderr, "hook-lock-failed", "Mark", "set dirty flag: "+err.Error(), nil)
		fmt.Fprintf(stderr, "engine-sync hook claude mark: set the dirty flag: %v\n", err)
	}
	return 0
}

// stopStages are the four stages a Stop runs, in order, each named as the
// block reason names it.
func stopStages(root string, engines []string) [][]string {
	var stages [][]string
	for _, engine := range engines {
		stages = append(stages, []string{engine, "build", root}, []string{engine, "check", root})
	}
	return stages
}

func Stop(payload Payload, stdout, stderr io.Writer, e Context, runner StageRunner) int {
	if payload.SessionID != "" {
		e.SessionID = payload.SessionID
	}
	notice := func(event, message string) {
		recordHook(e, stderr, event, "Stop", message, nil)
		hookNotice(stdout, stderr, message)
	}
	cwd, err := os.Getwd()
	if err != nil {
		notice("hook-step-failed", "engine-sync hook claude stop: getwd: "+err.Error())
		return 0
	}
	root, rootErr := resolveProjectRoot(e, cwd, true)
	if rootErr != nil {
		notice("hook-step-failed", "engine-sync hook claude stop: "+rootErr.Error())
		return 0
	}
	if root == "" {
		return 0
	}
	flag, err := DirtyFlag(e, root)
	if err != nil {
		notice("hook-lock-failed", "engine-sync hook claude stop: "+err.Error())
		return 0
	}
	info, err := os.Lstat(flag)
	if err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = fmt.Errorf("dirty flag %s is not a regular file", flag)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			message := fmt.Sprintf("engine-sync: cannot inspect dirty flag: %v", err)
			recordHook(e, stderr, "hook-lock-failed", "Stop", message, nil)
			fmt.Fprintln(stderr, message)
			if err := json.NewEncoder(stdout).Encode(map[string]string{"systemMessage": message}); err != nil {
				fmt.Fprintln(stderr, err)
			}
		}
		return 0
	}
	// Keep the durable flag while stages run; a killed process cannot lose it.
	var failed []string
	var detail strings.Builder
	for _, stage := range stopStages(root, e.Engines) {
		code, output := runner(stage)
		if code == 0 {
			continue
		}
		name := stage[0] + " " + stage[1]
		recordHook(e, stderr, "hook-step-failed", "Stop", name+" failed", map[string]any{"stage": name, "code": code})
		failed = append(failed, name)
		detail.WriteString(name + ":\n" + output + "\n")
	}
	if len(failed) == 0 {
		home, err := e.Home()
		if err != nil {
			notice("hook-step-failed", "engine-sync: "+err.Error())
			return 0
		}
		release, err := guard.Lock(e.State(home), "dirty:"+root, 250*time.Millisecond)
		if err != nil {
			notice("hook-lock-failed", "engine-sync: "+err.Error())
			return 0
		}
		current, err := os.Lstat(flag)
		if err == nil && os.SameFile(info, current) && info.ModTime() == current.ModTime() && info.Size() == current.Size() {
			err = os.Remove(flag)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			notice("hook-lock-failed", "engine-sync: clear dirty flag: "+err.Error())
		}
		if err := release(); err != nil {
			notice("hook-lock-failed", "engine-sync: release dirty lock: "+err.Error())
		}
		return 0
	}
	stages := strings.Join(failed, ", ") + " (binary: " + e.Writer() + ")"
	text := detail.String()
	if len(text) > detailLimit {
		text = text[:detailLimit]
	}
	var answer any
	if payload.StopHookActive || strings.Contains(detail.String(), "BUSY ") {
		answer = map[string]string{"systemMessage": "engine-sync WARNING: " + stages +
			" failed — the mirror is stale; the flag stays set and the check repeats next turn.\n" + text}
	} else {
		answer = map[string]string{"decision": "block", "reason": "engine-sync: " + stages +
			" failed after this turn's source edits — the mirror is stale. Repair it now, then end the turn: " +
			"a dangling source link → repair its named source; a stale or orphan mirror file → " + shellPath(e.Writer()) + " build .; " +
			"then " + shellPath(e.Writer()) + " check . must pass. Output of the failing stage(s):\n" + text}
	}
	if err := json.NewEncoder(stdout).Encode(answer); err != nil {
		recordHook(e, stderr, "hook-step-failed", "Stop", "write answer: "+err.Error(), nil)
		fmt.Fprintf(stderr, "engine-sync hook claude stop: write the answer: %v\n", err)
	}
	return 0
}

// Start rebuilds a stale mirror (a `git pull` lands here) and refreshes
// the machine targets when the global file's `targets` names them. A build that fails sets
// the dirty flag, so the session's first Stop reports it.
func Start(stderr io.Writer, e Context, runner StageRunner) int {
	cwd, err := os.Getwd()
	if err != nil {
		recordHook(e, stderr, "hook-step-failed", "Start", "getwd: "+err.Error(), nil)
		fmt.Fprintf(stderr, "engine-sync hook claude start: getwd: %v\n", err)
		return 0
	}
	root, rootErr := resolveProjectRoot(e, cwd, true)
	if rootErr != nil {
		recordHook(e, stderr, "hook-step-failed", "Start", rootErr.Error(), nil)
		fmt.Fprintf(stderr, "engine-sync hook claude start: %v\n", rootErr)
	}
	if root != "" {
		if scoped, err := InScope(root); err != nil {
			recordHook(e, stderr, "hook-step-failed", "Start", err.Error(), nil)
			fmt.Fprintf(stderr, "engine-sync hook claude start: %v\n", err)
		} else if scoped {
			if code, _ := runner([]string{"check", root}); code != 0 {
				if code, output := runner([]string{"build", root}); code != 0 {
					recordHook(e, stderr, "hook-step-failed", "Start", "build failed", nil)
					fmt.Fprintf(stderr, "engine-sync hook claude start: build %s failed:\n%s", root, output)
					flag, flagErr := DirtyFlag(e, root)
					if flagErr == nil {
						flagErr = os.MkdirAll(filepath.Dir(flag), 0700)
					}
					if flagErr == nil {
						flagErr = atomicfile.Write(flag, []byte(root+"\n"), 0600)
					}
					if flagErr != nil {
						recordHook(e, stderr, "hook-lock-failed", "Start", "set dirty flag: "+flagErr.Error(), nil)
						fmt.Fprintf(stderr, "engine-sync hook claude start: set the dirty flag: %v\n", flagErr)
					}
				}
			}
		}
	}
	home, err := e.Home()
	if err != nil {
		recordHook(e, stderr, "hook-step-failed", "Start", err.Error(), nil)
		return 0
	}
	if found, err := e.Targets(home); err != nil {
		recordHook(e, stderr, "hook-step-failed", "Start", err.Error(), nil)
		fmt.Fprintf(stderr, "engine-sync hook claude start: %v\n", err)
	} else if found {
		if code, output := runner([]string{"global"}); code != 0 {
			recordHook(e, stderr, "hook-step-failed", "Start", "global failed", nil)
			fmt.Fprintf(stderr, "engine-sync hook claude start: global failed:\n%s", output)
		}
	}
	return 0
}

func shellPath(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'" }

// hookLogEnv uses the hook's resolved home even for an injected minimal environment.
func hookLogEnv(e Context) paths.Env {
	return func(key string) string {
		value := ""
		if e.Get != nil {
			value = e.Get(key)
		}
		if key == "HOME" && value == "" && e.Home != nil {
			home, err := e.Home()
			if err == nil {
				return home
			}
		}
		return value
	}
}

func recordHook(e Context, stderr io.Writer, event, step, msg string, detail any) {
	pluginlog.Record(hookLogEnv(e), stderr, pluginlog.Row{Level: "error", Event: event, Msg: msg, Where: "internal/hook/hook.go#" + step, Session: e.SessionID, Detail: detail})
}
