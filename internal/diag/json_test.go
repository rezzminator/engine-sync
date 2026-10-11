package diag

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestJSONResultKeepsArraysAndRunFailure(t *testing.T) {
	var out, errout bytes.Buffer
	rc := WriteJSON(&out, &errout, Report{OK: true}, errors.New("cannot read source"))
	var got JSONResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rc != 1 || got.OK || got.Error != "cannot read source" || got.Problems == nil || got.Rebuildable == nil || got.Warnings == nil {
		t.Fatalf("%d %+v", rc, got)
	}
}
