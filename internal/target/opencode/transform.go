package opencode

import "strings"

func openCodeFlatName(rel string) string {
	return strings.ReplaceAll(strings.TrimSuffix(strings.ReplaceAll(rel, "\\", "/"), ".md"), "/", "-")
}
