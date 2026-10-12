package cli

import (
	"fmt"
	"io"

	"github.com/rezzminator/engine-sync/internal/machine"
	"github.com/rezzminator/engine-sync/internal/source"
)

// runVariants is `engine-sync variants pin [name …]`: it recomputes the
// section pins of the named variants (every variant when none is named) and
// rewrites source pins blocks in place, keeping every other byte. It
// prints each pin it changes.
func runVariants(args []string, stdout, stderr io.Writer, e env) int {
	const usage = "usage: engine-sync variants pin [name]... [--home H] [--source-repo R]"
	if len(args) == 0 || args[0] != "pin" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	flags := NewFlagSet("variants pin", usage, stderr)
	home := flags.String("home", "", "the home whose machine agents directory holds source declarations")
	sourceRepo := flags.String("source-repo", "", "the repository whose configured agents directory holds source declarations")
	names, code, ok := ParseFlagsAnywhere(flags, args[1:])
	if !ok {
		return code
	}
	resolvedHome, err := absoluteHome(*home, e)
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync variants pin: resolve home: %v\n", err)
		return 1
	}
	policy, err := e.policy("", resolvedHome, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// The source repository resolves as `global` resolves it: the flag, else
	// targets.json, else the Claude config dir's agents.
	options, err := globalOptions(e, resolvedHome, *sourceRepo, "", nil, nil, false)
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync variants pin: %v\n", err)
		return 1
	}
	agentsDir := source.AgentsDirectory(resolvedHome, options.SourceRepo, *policy)
	// The pin runs under its agents directory's writer guard: a second pin
	// waits, then re-reads the declaration the first one rewrote.
	code, err = guarded(e, []string{machine.GlobalRoot(agentsDir)}, true, func() int {
		changes, err := source.PinGlobalAgentVariants(agentsDir, names)
		for _, change := range changes {
			fmt.Fprintf(stdout, "pinned %s § %s %s\n", change.Variant, change.Heading, change.Pin)
		}
		if err != nil {
			fmt.Fprintf(stderr, "engine-sync variants pin: %v\n", err)
			return 1
		}
		return 0
	})
	if err != nil {
		fmt.Fprintf(stderr, "engine-sync variants pin: %v\n", err)
		return 1
	}
	return code
}
