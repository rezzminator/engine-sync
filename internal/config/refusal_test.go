package config

import (
	"github.com/rezzminator/engine-sync/internal/artifact"
	"github.com/rezzminator/engine-sync/internal/testjail"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// machine.managedBy is the installer's name: the schema pattern, the loader
// and the artifact helper share one rule.
func TestManagedByRule(t *testing.T) {
	field, _ := reflect.TypeFor[Machine]().FieldByName("ManagedBy")
	if got := field.Tag.Get("pattern"); got != artifact.ManagedByPattern {
		t.Fatalf("machine.managedBy pattern %q, artifact's %q", got, artifact.ManagedByPattern)
	}
	for value, ok := range map[string]bool{"professor": true, "a": true, "pro-fessor2": true, "Pro fessor": false, "1pro": false, "pro_fessor": false, strings.Repeat("a", 33): false, strings.Repeat("a", 32): true} {
		home := testjail.Scratch(t)
		path := MachinePath(home)
		putConfigFile(t, path, `{"version":1,"machine":{"managedBy":`+strconv.Quote(value)+`}}`)
		cfg, e := Resolve("", home, func(string) string { return "" })
		if ok && (e != nil || cfg.Machine.ManagedBy != value) {
			t.Errorf("managedBy %q = %v %q, want accepted", value, e, cfg.Machine.ManagedBy)
		}
		if !ok && (e == nil || !IsRefusal(e) || !strings.Contains(e.Error(), "machine.managedBy") || !strings.Contains(e.Error(), path)) {
			t.Errorf("managedBy %q = %v (refusal %v), want a refusal naming machine.managedBy and %s", value, e, IsRefusal(e), path)
		}
	}
}

func TestRetiredStoreKeysAreRefused(t *testing.T) {
	for _, key := range []string{"generatedAgentsDir", "legacyCodexAgentsDir"} {
		home := testjail.Scratch(t)
		path := MachinePath(home)
		putConfigFile(t, path, `{"version":1,"machine":{"`+key+`":".local/state/pfm/generated/claude-agents"}}`)
		_, e := Resolve("", home, func(string) string { return "" })
		if e == nil || !IsRefusal(e) || !strings.Contains(e.Error(), "machine."+key+" is removed") || !strings.Contains(e.Error(), path) {
			t.Errorf("%s = %v (refusal %v), want machine.%s is removed naming %s", key, e, IsRefusal(e), key, path)
		}
	}
}
