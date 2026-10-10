package diag

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/rezzminator/engine-sync/internal/reconcile"
	"testing"
)

func TestPlanReportsFailureAndActions(t *testing.T) {
	var out, errout bytes.Buffer
	rc := WritePlan(&out, &errout, Report{OK: true}, []reconcile.Action{{Kind: "write", Path: "output"}}, errors.New("blocked"), 1)
	var p planJSON
	if err := json.Unmarshal(out.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if rc != 1 || p.OK || p.Error != "blocked" || len(p.Actions) != 1 || p.Problems == nil {
		t.Fatalf("%d %#v", rc, p)
	}
}
