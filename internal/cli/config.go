package cli

import (
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/cliargs"
	"github.com/rezzminator/engine-sync/internal/config"
	"io"
)

func runConfig(args []string, stdout, stderr io.Writer, e env) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: engine-sync config show|validate|schema [--json] [root]")
		return 2
	}
	if args[0] == "schema" {
		if len(args) != 1 && !(len(args) == 2 && args[1] == "--json") {
			fmt.Fprintln(stderr, "usage: engine-sync config schema [--json]")
			return 2
		}
		_, err := stdout.Write(config.Schema())
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if args[0] != "show" && args[0] != "validate" {
		fmt.Fprintln(stderr, "usage: engine-sync config show|validate|schema [--json] [root]")
		return 2
	}
	flags := cliargs.NewFlagSet("config", "usage: engine-sync config show|validate [--json] [root]", stderr)
	asJSON := flags.Bool("json", false, "print effective settings and provenance as JSON")
	rest, code, ok := cliargs.ParseFlagsAnywhere(flags, args[1:])
	if !ok {
		return code
	}
	if len(rest) > 1 {
		return 2
	}
	root, err := repoRoot(rest, e)
	home, homeErr := e.home()
	if err == nil {
		err = homeErr
	}
	var cfg *config.Config
	if err == nil {
		cfg, err = e.policy(root, home, stderr)
	}
	if err != nil {
		if *asJSON {
			_ = json.NewEncoder(stdout).Encode(map[string]any{"ok": false, "config": nil, "sources": []config.SourceRow{}, "error": err.Error()})
		} else {
			fmt.Fprintln(stderr, err)
		}
		return cliargs.FailureCode(err)
	}
	if *asJSON {
		err = json.NewEncoder(stdout).Encode(struct {
			OK      bool               `json:"ok"`
			Config  config.MachineFile `json:"config"`
			Sources []config.SourceRow `json:"sources"`
			Error   string             `json:"error"`
		}{true, config.Effective(*cfg), config.Describe(*cfg), ""})
	} else if args[0] == "validate" {
		_, err = fmt.Fprintln(stdout, "VALID "+root)
	} else {
		for _, row := range config.Describe(*cfg) {
			_, err = fmt.Fprintf(stdout, "%s\t%s\t%s\n", row.Key, row.Value, row.Source)
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
