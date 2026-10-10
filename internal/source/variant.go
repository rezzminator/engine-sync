package source

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"regexp"
	"sort"
	"strings"
)

var variantKey = regexp.MustCompile(`^([A-Za-z-]+):`)
var plainScalar = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// renderGlobalAgentVariant rewrites only the frontmatter lines it is told to:
// `name:` and each override key, a multi-line value's continuation lines
// going with its key. Every other frontmatter line — comments, unknown keys —
// is carried through byte-identical, and so is the body but for the swaps,
// each applied in key order to text that must occur in the body exactly once.
func RenderVariant(source, name, markerSource string, overrides, swaps map[string]string) (string, error) {
	lines := strings.Split(source, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return "", fmt.Errorf("no frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", fmt.Errorf("frontmatter starts with --- but has no closing fence")
	}
	replacements := map[string]string{"name": name}
	for key, value := range overrides {
		replacements[key] = value
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, "---",
		artifact.Marker(artifact.YAMLFront, markerSource))
	seen := make(map[string]bool, len(replacements))
	for i := 1; i < end; i++ {
		match := variantKey.FindStringSubmatch(lines[i])
		if match == nil {
			out = append(out, lines[i])
			continue
		}
		value, replace := replacements[match[1]]
		if !replace {
			out = append(out, lines[i])
			continue
		}
		if seen[match[1]] {
			return "", fmt.Errorf("frontmatter carries %q twice", match[1])
		}
		seen[match[1]] = true
		out = append(out, match[1]+": "+variantScalar(value))
		for i+1 < end && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
			i++
		}
	}
	for key := range replacements {
		if !seen[key] {
			return "", fmt.Errorf("source frontmatter has no %q to override", key)
		}
	}
	body := strings.Join(lines[end+1:], "\n")
	texts := make([]string, 0, len(swaps))
	for text := range swaps {
		texts = append(texts, text)
	}
	sort.Strings(texts)
	for _, text := range texts {
		if count := strings.Count(body, text); count != 1 {
			return "", fmt.Errorf(
				"%q text %q occurs %d times in the body, want exactly 1",
				"replace",
				text,
				count,
			)
		}
		body = strings.Replace(body, text, swaps[text], 1)
	}
	out = append(out, "---", body)
	return strings.Join(out, "\n"), nil
}

func variantScalar(value string) string {
	if plainScalar.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
