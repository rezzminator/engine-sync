package codex

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
)

// globalRoleHeader marks a machine role, which engine-sync global writes.
func globalRoleHeader(source string) string {
	return artifact.GlobalMarker(artifact.TOML, source) + "\n"
}
