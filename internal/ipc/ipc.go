// Package ipc defines the simple JSON protocol used between the clara CLI
// and the clarad daemon over a Unix Domain Socket.
package ipc

// Method constants for the control socket protocol.
const (
	MethodShutdown      = "shutdown"
	MethodStatus        = "status"
	MethodList          = "list"
	MethodStart         = "start"
	MethodStop          = "stop"
	MethodToolList      = "tool_list"
	MethodToolShow      = "tool_show"
	MethodToolCall      = "tool_call"
	MethodMCPRegister   = "mcp.register"
	MethodMCPUnregister = "mcp.unregister"
	MethodMCPList       = "mcp.list"
	MethodMCPStart      = "mcp.start"
	MethodMCPStop       = "mcp.stop"
	MethodMCPRestart    = "mcp.restart"
	MethodMCPAdd        = "mcp.add"
	MethodMCPRemove     = "mcp.remove"
	MethodEvents        = "events"
	MethodTUIHistory    = "tui.history"
	MethodTUIClear      = "tui.clear"
	MethodTUIAnswer     = "tui.answer"
)

// Request is a command sent from the CLI to the daemon.
type Request struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
	Args   map[string]any `json:"args,omitempty"` // Added for function arguments
	Data   any            `json:"data,omitempty"`
}

// Response is the daemon's reply to a CLI Request.
type Response struct {
	// Message is a human-readable status string.
	Message string `json:"message,omitempty"`
	// Data carries structured payload (e.g. intent list, status info).
	Data any `json:"data,omitempty"`
	// Error is non-empty when the daemon encountered an error.
	Error string `json:"error,omitempty"`
}
