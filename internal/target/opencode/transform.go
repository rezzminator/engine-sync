package opencode

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/rewrite"
	"strings"
)

func generatedMarker(source string) string {
	return artifact.Line(source)
}

func hasMarker(content string) bool {
	return artifact.Claims(content)
}

func openCodeFlatName(rel string) string {
	return strings.ReplaceAll(strings.TrimSuffix(strings.ReplaceAll(rel, "\\", "/"), ".md"), "/", "-")
}

func swapOpenCodeCommands(text string, roster map[string]string) string {
	return rewrite.Builtins().Apply(text, rewrite.Context{Engine: "opencode", Roster: roster})
}
