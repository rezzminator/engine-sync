package testjail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(RunTests(m)) }
func TestJailedScratchUsesCanonicalBase(t *testing.T) {
	base, err := canonicalBase()
	if err != nil {
		t.Fatal(err)
	}
	scratch := Scratch(t)
	if !strings.HasPrefix(scratch, base+string(filepath.Separator)) || !strings.HasPrefix(os.Getenv("HOME"), base+string(filepath.Separator)) {
		t.Fatalf("%s %s", scratch, os.Getenv("HOME"))
	}
}
func TestJailRefusesNestedOperatorHome(t *testing.T) {
	if refuseRealHome("/fixture/operator/jail", "/fixture/operator") == nil {
		t.Fatal("nested home accepted")
	}
	if err := refuseRealHome("/fixture/jail", "/fixture/operator"); err != nil {
		t.Fatal(err)
	}
}
