package cli

import (
	"bytes"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/hook"
	"github.com/rezzminator/engine-sync/internal/target"
	"io"
	"time"
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
	if args[1] != "mark" && args[1] != "stop" && args[1] != "start" {
		fmt.Fprintln(stderr, "usage: engine-sync hook claude mark|stop|start")
		return 2
	}
	payload := readHookPayload(e.stdin)
	e.sessionID = payload.SessionID
	if args[1] == "mark" && payload.ToolInput.FilePath == "" {
		return 0
	}
	e.lockWait = 250 * time.Millisecond
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
	names := []string{}
	for _, t := range target.Registry {
		names = append(names, t.Name())
	}
	return hook.Context{Version: Version, SessionID: e.sessionID, Get: e.get, Home: e.home, State: e.state, Policy: e.policy, Engines: names, Targets: func(home string) (bool, error) {
		cfg, err := e.policy("", home, io.Discard)
		if err != nil {
			return false, err
		}
		_, err = config.ResolveTargets(e.get, home, *cfg, config.Targets{})
		return true, err
	}, Writer: writerPath}

}
func readHookPayload(r io.Reader) hookPayload          { return hook.ReadPayload(r) }
func hookDirtyFlag(e env, root string) (string, error) { return hook.DirtyFlag(hookContext(e), root) }
func hookMark(p hookPayload, w io.Writer, e env) int   { return hook.Mark(p, w, hookContext(e)) }
func hookStop(p hookPayload, o, w io.Writer, e env, r stageRunner) int {
	return hook.Stop(p, o, w, hookContext(e), hook.StageRunner(r))
}
func hookStart(w io.Writer, e env, r stageRunner) int {
	return hook.Start(w, hookContext(e), hook.StageRunner(r))
}
