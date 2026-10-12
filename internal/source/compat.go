package source

import (
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var topLevelValue = regexp.MustCompile(`^([^\s#:\-][^:]*):[ \t]+(\S.*)$`)

// compatibleHeader is the one compatibility retry after a strict parse fails:
// each top-level single-line `key: value` whose line does not parse on its
// own as a one-key YAML mapping becomes `key: "{trimmed value}"`, the plain
// values Claude Code reads and strict YAML refuses (`description: Review: run
// the checks`, `argument-hint: [pr-number] [priority]`). It reports whether a
// line changed.
func compatibleHeader(header string) (string, bool) {
	lines := strings.Split(header, "\n")
	changed := false
	for i, line := range lines {
		m := topLevelValue.FindStringSubmatch(line)
		if m == nil || (i+1 < len(lines) && continuesValue(lines[i+1])) {
			continue
		}
		var one map[string]any
		if err := yaml.Unmarshal([]byte(line), &one); err == nil && len(one) == 1 {
			continue
		}
		lines[i] = m[1] + ": " + strconv.Quote(strings.TrimSpace(m[2]))
		changed = true
	}
	return strings.Join(lines, "\n"), changed
}

// continuesValue reports a non-blank indented line: the value above it spans
// more than one line.
func continuesValue(line string) bool {
	return strings.TrimSpace(line) != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"))
}
