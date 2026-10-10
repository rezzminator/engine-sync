package source

import (
	"testing"
)

func TestMCPDecodePreservesSortedServersAndUnknownFields(t *testing.T) {
	got, err := DecodeMCP([]byte(`{"mcpServers":{"z":{"command":"tool","custom":true},"a":{"url":"https://example.com"}}}`))
	if err != nil || len(got) != 2 || got[0].Name != "a" || string(got[1].Fields["custom"]) != "true" {
		t.Fatalf("%+v %v", got, err)
	}
}
