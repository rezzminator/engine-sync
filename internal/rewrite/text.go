package rewrite

import (
	"regexp"
	"sort"
	"strings"
)

func ModelsIn(text string, modelMap map[string]string) string {
	if len(modelMap) == 0 {
		return text
	}
	keys := make([]string, 0, len(modelMap))
	for key := range modelMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		if key == "" {
			continue
		}
		pattern := regexp.MustCompile(`(^|[^\w-])` + regexp.QuoteMeta(key) + `([^\w-]|$)`)
		text = pattern.ReplaceAllStringFunc(text, func(match string) string {
			start := 0
			if match != "" && !isModelChar(match[0]) {
				start = 1
			}
			end := len(match)
			if end > start && !isModelChar(match[end-1]) {
				end--
			}
			return match[:start] + modelMap[key] + match[end:]
		})
	}
	return text
}

func isModelChar(b byte) bool {
	return b == '_' || b == '-' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func CommandsIn(text string, commands map[string]string, naming func(string) string) string {
	if len(commands) == 0 {
		return text
	}
	keys := make([]string, 0, len(commands))
	for key := range commands {
		keys = append(keys, strings.TrimPrefix(key, "/"))
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	var out strings.Builder
	for index := 0; index < len(text); {
		if text[index] != '/' || (index > 0 && PrefixChar(text[index-1])) {
			out.WriteByte(text[index])
			index++
			continue
		}
		matched, end := "", -1
		for _, key := range keys {
			name := strings.TrimPrefix(key, "/")
			end = idEnd(text, index+1, name)
			if end < 0 {
				continue
			}
			matched = name
			break
		}
		if matched == "" {
			out.WriteByte(text[index])
			index++
			continue
		}
		replacement, ok := commands[matched]
		if !ok {
			replacement = commands["/"+matched]
		}
		replacement += prefixRest(text, index+1, matched, end)
		if naming != nil {
			replacement = naming(replacement)
		}
		out.WriteString(replacement)
		index = end
	}
	return out.String()
}

// AgentsIn swaps each Claude plugin agent id of agents ("{plugin}:{name}")
// for its twin name, longest id first. A match is bounded on both sides: no
// name character, ':' or path character before it (a "/" makes it a
// command), and the command suffix rule after it, so "flights:lander" never
// touches "flights:lander-x" or "flights:lander.md". A key ending in ":"
// is a plugin prefix (a plugin whose records could not be read): it matches
// any id of that plugin and keeps the rest of the id, ":" flattened to "-".
func AgentsIn(text string, agents map[string]string) string {
	if len(agents) == 0 {
		return text
	}
	keys := make([]string, 0, len(agents))
	for key := range agents {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j]) || len(keys[i]) == len(keys[j]) && keys[i] < keys[j]
	})
	var out strings.Builder
	for index := 0; index < len(text); {
		matched, end := "", -1
		if index == 0 || !(PrefixChar(text[index-1]) || text[index-1] == ':') {
			for _, key := range keys {
				if end = idEnd(text, index, key); end >= 0 {
					matched = key
					break
				}
			}
		}
		if matched == "" {
			out.WriteByte(text[index])
			index++
			continue
		}
		out.WriteString(agents[matched] + prefixRest(text, index, matched, end))
		index = end
	}
	return out.String()
}

func PrefixChar(value byte) bool {
	return WordChar(value) || value == '/' || value == '.' || value == '-'
}

func SuffixChar(value byte) bool {
	// A slash command is followed by whitespace, end-of-line, or punctuation
	// — never by another "/". Without this, a filesystem path that merely
	// starts with a command name (/dev/tty) gets rewritten right along with
	// the actual command invocation (/dev build engine-sync).
	return WordChar(value) || value == ':' || value == '-' || value == '/'
}

func WordChar(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

// idEnd is where the id key, matched at text[start:], ends, or -1 when it
// does not match there. A key ending in ":" is a prefix that takes the id's
// name characters after it; every id is then bounded by the command suffix
// rule, so "x:y" never matches "x:y-z", "x:y/z" or "x:y.md".
func idEnd(text string, start int, key string) int {
	end := start + len(key)
	if end > len(text) || text[start:end] != key {
		return -1
	}
	if strings.HasSuffix(key, ":") {
		for end < len(text) && (WordChar(text[end]) || text[end] == '-' || text[end] == ':' && end+1 < len(text) && WordChar(text[end+1])) {
			end++
		}
		if end == start+len(key) {
			return -1
		}
	}
	if end < len(text) && (SuffixChar(text[end]) || (text[end] == '.' && end+1 < len(text) && WordChar(text[end+1]))) {
		return -1
	}
	return end
}

// prefixRest is what a prefix key matched past itself, flattened as twin
// names are (":" to "-"); an exact key leaves nothing.
func prefixRest(text string, start int, key string, end int) string {
	if !strings.HasSuffix(key, ":") {
		return ""
	}
	return strings.ReplaceAll(text[start+len(key):end], ":", "-")
}
