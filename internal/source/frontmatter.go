package source

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Frontmatter struct {
	Description, Model, Effort string
	Tools                      []string
	Engine                     map[string]map[string]string
	Fields                     map[string]string
}

func Parse(text string) (Frontmatter, string, error) {
	fields, body, err := Fields(text)
	front := Frontmatter{Description: fields["description"], Model: fields["model"], Effort: fields["effort"], Engine: map[string]map[string]string{}, Fields: fields}
	for _, tool := range strings.Split(fields["tools"], ",") {
		if tool = strings.TrimSpace(tool); tool != "" {
			front.Tools = append(front.Tools, tool)
		}
	}
	for key, value := range fields {
		if engine, setting, ok := strings.Cut(key, "-"); ok && engine != "" && setting != "" {
			if front.Engine[engine] == nil {
				front.Engine[engine] = map[string]string{}
			}
			front.Engine[engine][setting] = value
		}
	}
	return front, body, err
}

var (
	frontmatterScalar = regexp.MustCompile(`^([A-Za-z-]+):\s*([>|])[-+]?\s*$`)
	frontmatterField  = regexp.MustCompile(`^([A-Za-z-]+):\s*(.*)$`)
)

// parseFrontmatter reads the two-fence YAML subset used by command files. It
// deliberately rejects malformed fences: silently treating a malformed
// header as body changes both the generated frontmatter and its invocation.
func Fields(text string) (map[string]string, string, error) {
	lines := strings.Split(normalizeFrontmatter(text), "\n")
	fields := make(map[string]string)
	if !utf8.ValidString(text) {
		return nil, "", fmt.Errorf("source is not UTF-8")
	}
	if strings.IndexByte(text, 0) >= 0 {
		return nil, "", fmt.Errorf("source contains NUL")
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fields, text, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, "", fmt.Errorf("frontmatter starts with --- but has no closing fence")
	}
	for i := 1; i < end; i++ {
		line := lines[i]
		if match := frontmatterScalar.FindStringSubmatch(line); match != nil {
			key, style := match[1], match[2]
			var chunk []string
			indent := -1
			for i+1 < end {
				next := lines[i+1]
				if strings.TrimSpace(next) == "" {
					chunk = append(chunk, "")
					i++
					continue
				}
				leading := len(next) - len(strings.TrimLeft(next, " \t"))
				if leading == 0 || (indent >= 0 && leading < indent) {
					break
				}
				if indent < 0 {
					indent = leading
				}
				chunk = append(chunk, next[indent:])
				i++
			}
			for len(chunk) > 0 && chunk[len(chunk)-1] == "" {
				chunk = chunk[:len(chunk)-1]
			}
			if style == ">" {
				fields[key] = strings.Join(chunk, " ")
			} else {
				fields[key] = strings.Join(chunk, "\n")
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		match := frontmatterField.FindStringSubmatch(line)
		if match == nil {
			// Claude frontmatter may carry richer YAML mappings (hooks, nested
			// tool policy) that this compiler neither consumes nor rewrites.
			// The incumbents ignore those continuation lines; rejecting them
			// would make a valid Claude agent impossible to mirror.
			continue
		}
		rawValue := match[2]
		if strings.TrimSpace(rawValue) == "" {
			var items []string
			for i+1 < end && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "- ") {
				items = append(items, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i+1]), "- ")))
				i++
			}
			for n, item := range items {
				value, e := UnquoteScalar(item)
				if e != nil {
					return nil, "", fmt.Errorf("frontmatter field %s: %w", match[1], e)
				}
				items[n] = value
			}
			rawValue = strings.Join(items, ", ")
		}
		if rawValue == "" || rawValue[0] != '\'' && rawValue[0] != '"' {
			if comment := strings.Index(rawValue, " #"); comment >= 0 {
				rawValue = strings.TrimSpace(rawValue[:comment])
			}
		}
		value, err := UnquoteScalar(rawValue)
		if err != nil {
			return nil, "", fmt.Errorf("frontmatter field %s: %w", match[1], err)
		}
		if match[1] == "tools" && strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
			parts := strings.Split(value, ",")
			for n, v := range parts {
				v, e := UnquoteScalar(strings.TrimSpace(v))
				if e != nil {
					return nil, "", e
				}
				parts[n] = v
			}
			value = strings.Join(parts, ", ")
		}
		fields[match[1]] = value
	}
	return fields, strings.Join(lines[end+1:], "\n"), nil
}

func UnquoteScalar(value string) (string, error) {
	if len(value) < 2 || value[0] != value[len(value)-1] {
		return value, nil
	}
	switch value[0] {
	case '\'':
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), nil
	case '"':
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("decode double-quoted YAML scalar: %w", err)
		}
		return unquoted, nil
	default:
		return value, nil
	}
}

func HeaderLines(text string) []string {
	lines := strings.Split(normalizeFrontmatter(text), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			out := make([]string, 0, i-1)
			for _, line := range lines[1:i] {
				if !strings.HasPrefix(line, "name:") {
					out = append(out, line)
				}
			}
			return out
		}
	}
	return nil
}

func ScalarField(line string) []string { return frontmatterField.FindStringSubmatch(line) }

func normalizeFrontmatter(text string) string {
	return strings.ReplaceAll(strings.TrimPrefix(text, "\ufeff"), "\r\n", "\n")
}
