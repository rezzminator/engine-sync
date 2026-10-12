package opencode

import (
	"encoding/json"
	"fmt"
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/diag"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	claudesource "github.com/rezzminator/engine-sync/internal/source"
	"path/filepath"
	"strings"
)

func (Target) Validate(artifacts []artifact.Artifact) []diag.Diagnostic {
	var diagnostics []diag.Diagnostic
	for _, a := range artifacts {
		if a.Kept != nil || a.Kind == artifact.Link {
			continue
		}
		message, headerErr := "", ""
		dir := filepath.Base(filepath.Dir(a.Path))
		if a.NativeMode != "" && a.NativeMode != "all" && a.NativeMode != "primary" && a.NativeMode != "subagent" {
			message = "invalid native agent mode " + a.NativeMode
		}
		if filepath.Base(a.Path) == "opencode.jsonc" {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(parseOpenCodeJSONC(a.Content), &object); err != nil {
				message = fmt.Sprintf("does not parse (%v)", err)
			}
		} else if strings.HasSuffix(a.Path, ".md") && (dir == "agent" || dir == "agents" || dir == "command") {
			front, _, err := claudesource.Parse(string(a.Content))
			fields := front.Fields
			if err != nil {
				headerErr = claudesource.DescribeParseError(a.Path, err)
			} else if strings.TrimSpace(fields["description"]) == "" {
				message = "empty description; OpenCode filters it out and the role/command goes silent"
			} else if filepath.Base(filepath.Dir(a.Path)) == "agent" && fields["mode"] != expectedOpenCodeMode(a) {
				message = fmt.Sprintf("mode %q is not %s; the compiler only emits %s roles", fields["mode"], expectedOpenCodeMode(a), expectedOpenCodeMode(a))
			}
		}
		if headerErr != "" {
			diagnostics = append(diagnostics, diag.Diagnostic{Severity: diag.Finding, State: reconcile.Invalid, Path: a.Path, Message: headerErr})
		} else if message != "" {
			diagnostics = append(diagnostics, diag.Diagnostic{Severity: func() diag.Severity {
				if filepath.Base(filepath.Dir(a.Path)) == "command" && strings.TrimSpace(message) == "empty description; OpenCode filters it out and the role/command goes silent" {
					return diag.Warning
				}
				return diag.Finding
			}(), State: reconcile.Invalid, Path: a.Path, Message: fmt.Sprintf("%s %s — %s", reconcile.Invalid, a.Path, message)})
		}
	}
	return diagnostics
}

func expectedOpenCodeMode(a artifact.Artifact) string {
	if a.NativeMode != "" {
		return a.NativeMode
	}
	return openCodeAgentMode
}
