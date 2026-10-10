package diag

import (
	"errors"
	"strings"
	"testing"
)

func TestDanglingDiagnosticNamesItsSource(t *testing.T) {
	var r Report
	r.RecordDangling("reader.md", errors.New("missing target"))
	if len(r.Dangling) != 1 || len(r.Warnings) != 1 || !strings.Contains(r.Dangling[0], "reader.md") {
		t.Fatalf("%+v", r)
	}
}
