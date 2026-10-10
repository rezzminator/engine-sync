package cliargs

import (
	"bytes"
	"testing"
)

func TestFlagsAfterPositionals(t *testing.T) {
	var out bytes.Buffer
	f := NewFlagSet("fixture", "usage", &out)
	v := f.String("value", "", "value")
	rest, rc, ok := ParseFlagsAnywhere(f, []string{"root", "--value", "selected"})
	if !ok || rc != 0 || len(rest) != 1 || rest[0] != "root" || *v != "selected" {
		t.Fatalf("%v %d %v %s", rest, rc, ok, *v)
	}
}
