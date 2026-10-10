package codex

import (
	"github.com/BurntSushi/toml"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"regexp"
	"sort"
	"strings"
)

func tomlEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\r", `\r`)
	return strings.ReplaceAll(value, "\n", `\n`)
}

func tomlMultiline(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"""`, `\"\"\"`)
	if !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	return value
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
