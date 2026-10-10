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
		matched := ""
		for _, key := range keys {
			name := strings.TrimPrefix(key, "/")
			end := index + 1 + len(name)
			if end > len(text) || text[index+1:end] != name {
				continue
			}
			if end < len(text) && SuffixChar(text[end]) {
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
		if naming != nil {
			replacement = naming(replacement)
		}
		out.WriteString(replacement)
		index += 1 + len(matched)
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
