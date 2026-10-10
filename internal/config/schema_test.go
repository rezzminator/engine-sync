package config

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The documented leaf-key table follows the same shape as schema generation.
func TestREADMEKeysMatchSchemaShape(t *testing.T) {
	raw, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	re := regexp.MustCompile("(?m)^\\| `([^`]+)` \\|")
	for _, row := range re.FindAllStringSubmatch(string(raw), -1) {
		seen[row[1]] = true
	}
	var walk func(reflect.Type, string)
	walk = func(typ reflect.Type, prefix string) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			key := prefix + name
			if f.Type.Kind() == reflect.Struct && f.Type != reflect.TypeOf(Permission{}) {
				walk(f.Type, key+".")
			} else if !seen[key] {
				t.Errorf("README key missing: %s", key)
			}
		}
	}
	walk(reflect.TypeOf(MachineFile{}), "")
}
