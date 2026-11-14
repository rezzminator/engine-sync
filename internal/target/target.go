package target

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/paths"
	"github.com/rezzminator/engine-sync/internal/source"
	"github.com/rezzminator/engine-sync/internal/target/codex"
	"github.com/rezzminator/engine-sync/internal/target/opencode"
)

type Target interface {
	Name() string
	Roots(source.Project, config.Config) []artifact.Root
	Render(source.Project, config.Config) ([]artifact.Artifact, []diag.Diagnostic)
	Validate([]artifact.Artifact) []diag.Diagnostic
}

var Registry = []Target{codex.Target{}, opencode.Target{}}

func Lookup(name string) (Target, bool) {
	for _, target := range Registry {
		if target.Name() == name {
			return target, true
		}
	}
	return nil, false
}

// Select is the targets a project run named engine ("" for every one) runs:
// skipped holds the selected engines $ENGINE_SYNC_DISABLE switches off,
// disabled every engine it names, selected or not. An unknown name in the
// variable is the error.
func Select(engine string, env paths.Env) (targets []Target, skipped, disabled []string, err error) {
	var names []string
	for _, t := range Registry {
		names = append(names, t.Name())
	}
	off, err := paths.DisabledEngines(env, names)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, t := range Registry {
		if off[t.Name()] {
			disabled = append(disabled, t.Name())
		}
		switch {
		case engine != "" && engine != t.Name():
		case off[t.Name()]:
			skipped = append(skipped, t.Name())
		default:
			targets = append(targets, t)
		}
	}
	return targets, skipped, disabled, nil
}
