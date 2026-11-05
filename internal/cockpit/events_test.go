package cockpit

import (
	"strings"
	"testing"
)

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input       string
		wantIsSlash bool
		wantCmd     string
		wantArgs    string
	}{
		{input: "/model gemini-3.8-flash-low", wantIsSlash: true, wantCmd: "model", wantArgs: "gemini-3.8-flash-low"},
		{input: "/edit", wantIsSlash: true, wantCmd: "edit", wantArgs: ""},
		{input: "/cancel", wantIsSlash: true, wantCmd: "cancel", wantArgs: ""},
		{input: "/interrupt", wantIsSlash: true, wantCmd: "interrupt", wantArgs: ""},
		{input: "/queue", wantIsSlash: true, wantCmd: "queue", wantArgs: ""},
		{input: "/detach", wantIsSlash: true, wantCmd: "detach", wantArgs: ""},
		{input: "just a normal prompt", wantIsSlash: false, wantCmd: "", wantArgs: ""},
		{input: "  /model foo  ", wantIsSlash: true, wantCmd: "model", wantArgs: "foo"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			isSlash, cmd, args := ParseSlashCommand(tc.input)
			if isSlash != tc.wantIsSlash {
				t.Fatalf("isSlash = %v, want %v", isSlash, tc.wantIsSlash)
			}
			if cmd != tc.wantCmd {
				t.Fatalf("cmd = %q, want %q", cmd, tc.wantCmd)
			}
			if args != tc.wantArgs {
				t.Fatalf("args = %q, want %q", args, tc.wantArgs)
			}
		})
	}
}

func TestRenderEvent(t *testing.T) {
	eUser := EventItem{
		Seq:     1,
		Type:    "message.user",
		Payload: map[string]any{"text": "Hello world"},
	}
	rendered := RenderEvent(eUser)
	if !strings.Contains(rendered, "You:") || !strings.Contains(rendered, "Hello world") {
		t.Fatalf("unexpected user message render: %q", rendered)
	}

	eAssistant := EventItem{
		Seq:     2,
		Type:    "message.assistant",
		Payload: map[string]any{"text": "I can help with that"},
	}
	renderedAss := RenderEvent(eAssistant)
	if !strings.Contains(renderedAss, "Agent:") || !strings.Contains(renderedAss, "I can help with that") {
		t.Fatalf("unexpected assistant message render: %q", renderedAss)
	}

	eTool := EventItem{
		Seq:     3,
		Type:    "tool.call",
		Payload: map[string]any{"tool": "fs_read", "args": map[string]any{"path": "foo.go"}},
	}
	renderedTool := RenderEvent(eTool)
	if !strings.Contains(renderedTool, "fs_read") {
		t.Fatalf("unexpected tool call render: %q", renderedTool)
	}
}
