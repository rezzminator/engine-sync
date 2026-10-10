package target

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/config"
	"github.com/rezzminator/engine-sync/internal/diag"
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
