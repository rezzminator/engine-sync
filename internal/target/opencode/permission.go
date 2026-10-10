package opencode

import (
	"fmt"
	"github.com/rezzminator/engine-sync/internal/config"
	"strings"
)

func renderAgentPermission(permission config.Permission) string {
	if permission.Action != "" {
		return "permission: " + permission.Action + "\n"
	}
	if len(permission.Tools) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("permission:\n")
	for _, tool := range permission.Tools {
		key, value := tool.Name, tool.Value
		if strings.ContainsAny(key, "*?:{}[] ,\n\t\"'&!#|>@`") {
			key = quoteOpenCodeYAML(key)
		}
		if value.Action != "" {
			fmt.Fprintf(&out, "  %s: %s\n", key, value.Action)
		} else {
			fmt.Fprintf(&out, "  %s:\n", key)
			for _, rule := range value.Rules {
				fmt.Fprintf(&out, "    %s: %s\n", quoteOpenCodeYAML(rule.Pattern), rule.Action)
			}
		}
	}
	return out.String()
}
