package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"regexp"
)

// removedKeys are configuration keys a former release read and this one
// refuses, each naming where its value lives now, so an old file fails loudly
// instead of being silently ignored or rejected as a bare unknown key.
var removedKeys = []struct{ section, key, replacement string }{
	{"machine", "modelMap", "use codex.modelMap"},
	{"machine", "variants", "use {machine.sourceAgentsDir}/{name}.md declarations"},
	{"opencode", "agent", "use engines.opencode.permission in each agent's frontmatter"},
	{"machine", "generatedAgentsDir", "set machine.sourceGeneratedAgentsDir for the tracked renders; there is no home store"},
	{"machine", "legacyCodexAgentsDir", "nothing: Codex roles install directly, there is no home store"},
}

// refuseRemoved names a removed key before shape validation reports it as
// unknown.
func refuseRemoved(object map[string]json.RawMessage) error {
	for _, removed := range removedKeys {
		raw, ok := object[removed.section]
		if !ok {
			continue
		}
		var section map[string]json.RawMessage
		if json.Unmarshal(raw, &section) != nil {
			continue
		}
		if _, ok := section[removed.key]; ok {
			return &Refusal{Msg: fmt.Sprintf("%s.%s is removed; %s", removed.section, removed.key, removed.replacement)}
		}
	}
	return nil
}

// Refusal is a configuration the loader refuses by name — a removed key, a
// moved file, a key in the wrong file, a malformed `machine.managedBy`; the
// CLI exits 2 on it, not 1.
type Refusal struct{ Msg string }

func (r *Refusal) Error() string { return r.Msg }

// IsRefusal reports whether err is, or wraps, a Refusal.
func IsRefusal(err error) bool {
	var refusal *Refusal
	return errors.As(err, &refusal)
}

var managedByValue = regexp.MustCompile(artifact.ManagedByPattern)
