package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/hook"
	"io"
	"os"
)

// stageRunner runs one engine-sync command and returns its exit code and
// combined output; the hooks run their stages in-process through run().
type stageRunner func(args []string) (int, string)

type hookPayload = hook.Payload

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
	home, err := e.home()
	if err != nil {
		fmt.Fprintln(stderr, err)
		_ = json.NewEncoder(stdout).Encode(map[string]string{"systemMessage": "engine-sync configuration failed: " + err.Error()})
		return 0
	}
	cwd, _ := os.Getwd()
	root := hookProjectRoot(e, cwd)
	if _, err := e.policy(root, home, stderr); err != nil {
		fmt.Fprintln(stderr, err)
		_ = json.NewEncoder(stdout).Encode(map[string]string{"systemMessage": "engine-sync configuration failed: " + err.Error()})
		return 0
	}
	payload := readHookPayload(e.stdin)
	runner := func(stage []string) (int, string) {
		var out bytes.Buffer
		code := dispatch(stage, &out, &out, e)
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

func hookContext(e env) hook.Context {
	return hook.Context{Get: e.get, Home: e.home, State: e.state, Policy: e.policy, Targets: func(home string) (bool, error) { _, found, err := readMachineTargets(e, home); return found, err }, Writer: writerPath}
}
func readHookPayload(r io.Reader) hookPayload          { return hook.ReadPayload(r) }
func hookProjectRoot(e env, dir string) string         { return hook.ProjectRoot(hookContext(e), dir) }
func hookDirtyFlag(e env, root string) (string, error) { return hook.DirtyFlag(hookContext(e), root) }
func hookMark(p hookPayload, w io.Writer, e env) int   { return hook.Mark(p, w, hookContext(e)) }
func hookStop(p hookPayload, o, w io.Writer, e env, r stageRunner) int {
	return hook.Stop(p, o, w, hookContext(e), hook.StageRunner(r))
}
func hookStart(w io.Writer, e env, r stageRunner) int {
	return hook.Start(w, hookContext(e), hook.StageRunner(r))
}
