package hook

import (
	"io"
	"strings"
	"testing"
)

func TestHookPayloadAcceptsToolAndStopForms(t *testing.T) {
	p := ReadPayload(strings.NewReader(`{"tool_input":{"file_path":"/fixture/.claude/agents/reader.md"},"stop_hook_active":true}`), io.Discard, Context{})
	if !p.StopHookActive || !IsClaudeSource(p.ToolInput.FilePath, "/fixture/home") {
		t.Fatalf("%+v", p)
	}
}
