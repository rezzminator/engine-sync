package hook

import (
	"strings"
	"testing"
)

func TestHookPayloadAcceptsToolAndStopForms(t *testing.T) {
	p := ReadPayload(strings.NewReader(`{"tool_input":{"file_path":"/fixture/.claude/agents/reader.md"},"stop_hook_active":true}`))
	if !p.StopHookActive || !IsClaudeSource(p.ToolInput.FilePath, "/fixture/home") {
		t.Fatalf("%+v", p)
	}
	if p = ReadPayload(strings.NewReader("malformed")); p.ToolInput.FilePath != "" || p.StopHookActive {
		t.Fatalf("%+v", p)
	}
}
