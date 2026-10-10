package codex

import (
	"fmt"
	"github.com/BurntSushi/toml"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"regexp"
	"sort"
	"strings"
)

func tomlEscape(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch r {
		case '\\':
			out.WriteString(`\\`)
		case '"':
			out.WriteString(`\"`)
		case '\r':
			out.WriteString(`\r`)
		case '\n':
			out.WriteString(`\n`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&out, `\u%04X`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}
func tomlMultiline(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r == '\\':
			out.WriteString(`\\`)
		case (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f:
			fmt.Fprintf(&out, `\u%04X`, r)
		default:
			out.WriteRune(r)
		}
	}
	result := strings.ReplaceAll(out.String(), `"""`, `\"\"\"`)
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

func tomlKey(key string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(key) {
		return key
	}
	return `"` + tomlEscape(key) + `"`
}

func tomlString(value string) string { return `"` + tomlEscape(value) + `"` }

// validateTOML uses a complete pure-Go parser. Check mode validates every TOML
// artifact Codex can load, including hand-written keeper files; a narrow check
// for only the syntax emitted here would confuse "could not look" with clean.
func validateTOML(data string) error {
	var document map[string]any
	_, err := toml.Decode(data, &document)
	return err
}

type mcpServer = claudesource.MCPServer

func codexMCPServers(input []claudesource.MCPServer) (map[string]mcpServer, map[string][]string) {
	servers := map[string]mcpServer{}
	unmapped := map[string][]string{}
	for _, server := range input {
		for key := range server.Fields {
			switch key {
			case "command", "args", "env", "url", "type":
			default:
				unmapped[server.Name] = append(unmapped[server.Name], key)
			}
		}
		sort.Strings(unmapped[server.Name])
		servers[server.Name] = server
	}
	return servers, unmapped
}
