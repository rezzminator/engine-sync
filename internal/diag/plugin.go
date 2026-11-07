package diag

import "github.com/rezzminator/engine-sync/internal/source"

// PluginFailed is a plugin whose records could not be read, as an error
// naming kept, the path of its Kept reservation: the build refuses that
// output (kept already) and still builds every other twin, never reading
// the failure as an empty plugin. An empty kept names no output, and the
// error stops the build.
func PluginFailed(failure source.PluginFailure, kept string) Diagnostic {
	return Diagnostic{Severity: Error, Path: kept, Message: failure.Problem + "; its twins are kept"}
}
