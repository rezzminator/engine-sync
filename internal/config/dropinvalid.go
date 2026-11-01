package config

import (
	"fmt"
	"strings"

	"github.com/rezzminator/engine-sync/internal/artifact"
)

// validName is a codex.neverRegister or machine.retiredAgents entry: one
// agent name, never a path.
func validName(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "/\\ \t\n") && value != "." && value != ".."
}

// dropInvalid removes, each with a warning naming its file, the file-layer
// values a run can go on without: an invalid list entry is that one entry,
// a malformed machine.managedBy writes no managed-by line.
func dropInvalid(cfg *Config) {
	warn := func(key, msg string) {
		cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("config %s: %s: %s", cfg.Sources[key], key, msg))
	}
	keep := func(key string, values []string, ok func(string) bool) []string {
		kept := values[:0:0]
		for _, value := range values {
			if ok(value) {
				kept = append(kept, value)
			} else {
				warn(key, fmt.Sprintf("invalid entry %q, ignored", value))
			}
		}
		return kept
	}
	cfg.Codex.NeverRegister = keep("codex.neverRegister", cfg.Codex.NeverRegister, validName)
	cfg.Machine.RetiredAgents = keep("machine.retiredAgents", cfg.Machine.RetiredAgents, validName)
	cfg.Agents.PassthroughKeys = keep("agents.passthroughKeys", cfg.Agents.PassthroughKeys, func(key string) bool {
		return strings.TrimSpace(key) != "" && !strings.ContainsAny(key, " \t\n:")
	})
	if cfg.Targets != nil {
		cfg.Targets.Capabilities = keep("targets.capabilities", cfg.Targets.Capabilities, func(name string) bool { return strings.TrimSpace(name) != "" })
	}
	if cfg.Machine.ManagedBy != "" && !managedByValue.MatchString(cfg.Machine.ManagedBy) {
		warn("machine.managedBy", fmt.Sprintf("%q must match %s; ignored, no managed-by line is written", cfg.Machine.ManagedBy, artifact.ManagedByPattern))
		cfg.Machine.ManagedBy = ""
	}
}
