package machine

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReportFailureAndRetirement(t *testing.T) {
	for _, json := range []bool{false, true} {
		var out, errout bytes.Buffer
		rc := PrintReport(Report{Events: []Event{{Verb: VerbRetire, Message: "delete owned"}}, Err: errors.New("write denied")}, json, &out, &errout)
		if rc != 1 || !strings.Contains(out.String()+errout.String(), "write denied") {
			t.Fatalf("%d %s %s", rc, &out, &errout)
		}
	}
}
