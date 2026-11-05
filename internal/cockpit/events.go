package cockpit

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	userStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))   // Cyan
	assistantStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))  // Magenta/Pink
	toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))             // Gray
	statusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))             // Yellow/Gold
	errorStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))  // Red
)

// EventItem represents a single event in the transcript.
type EventItem struct {
	Seq     int64          `json:"seq"`
	HostSeq int64          `json:"host_seq"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// ParseSlashCommand checks if an input line is a slash command and extracts command + arguments.
func ParseSlashCommand(input string) (bool, string, string) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return false, "", ""
	}
	parts := strings.SplitN(trimmed[1:], " ", 2)
	cmd := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return true, cmd, args
}

// RenderEvent converts an EventItem into a formatted terminal string.
func RenderEvent(e EventItem) string {
	switch e.Type {
	case "message.user":
		txt, _ := e.Payload["text"].(string)
		return fmt.Sprintf("%s\n%s\n", userStyle.Render("❯ You:"), txt)

	case "message.assistant":
		txt, _ := e.Payload["text"].(string)
		return fmt.Sprintf("%s\n%s\n", assistantStyle.Render("▲ Agent:"), txt)

	case "tool.call":
		toolName, _ := e.Payload["tool"].(string)
		args, _ := json.Marshal(e.Payload["args"])
		return toolStyle.Render(fmt.Sprintf("  ⚙ %s(%s)", toolName, string(args))) + "\n"

	case "tool.result":
		toolName, _ := e.Payload["tool"].(string)
		return toolStyle.Render(fmt.Sprintf("  ✓ %s done", toolName)) + "\n"

	case "turn.started":
		return statusStyle.Render("── Turn Started ──") + "\n"

	case "turn.ended":
		stopReason, _ := e.Payload["stop_reason"].(string)
		return statusStyle.Render(fmt.Sprintf("── Turn Ended (%s) ──", stopReason)) + "\n"

	case "session.status":
		state, _ := e.Payload["state"].(string)
		reason, _ := e.Payload["reason"].(string)
		if reason != "" {
			return statusStyle.Render(fmt.Sprintf("• State: %s (%s)", state, reason)) + "\n"
		}
		return statusStyle.Render(fmt.Sprintf("• State: %s", state)) + "\n"

	default:
		return ""
	}
}
