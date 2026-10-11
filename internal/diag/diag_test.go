package diag

import (
	"bytes"
	"testing"
)

func TestDiagnosticTextCarriesRemedy(t *testing.T) {
	var out bytes.Buffer
	Text(&out, []Diagnostic{{Severity: Finding, Message: "conflict at role", Remedy: "edit source"}})
	if out.String() != "conflict at role\nedit source\n" {
		t.Fatal(out.String())
	}
}
