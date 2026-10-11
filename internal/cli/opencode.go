package cli

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/target/opencode"
	"io"
)

func parseOpenCodeMode(action string) (opencode.Mode, bool) {
	switch action {
	case "build":
		return opencode.ModeBuild, true
	case "check":
		return opencode.ModeCheck, true
	case "doctor":
		return opencode.ModeDoctor, true
	default:
		return 0, false
	}
}

func printOpenCodeUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: engine-sync opencode build|check|doctor [repo-root] [options]")
	fmt.Fprintln(w, "  --home PATH    OpenCode global source/output home")
	fmt.Fprintln(w, "  --json         one JSON result object on stdout")
}
