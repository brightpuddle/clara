// Package ipc defines the simple JSON protocol used between the clara CLI
// and the clarad daemon over a Unix Domain Socket.
package ipc

// Method constants for the control socket protocol.
const (
	MethodShutdown     = "shutdown"
	MethodStatus       = "status"
	MethodList         = "list"
	MethodRun          = "run"
	MethodStart        = "start"
	MethodStop         = "stop"
	MethodToolList     = "tool_list"
	MethodToolShow     = "tool_show"
	MethodToolCall     = "tool_call"
	MethodEvents       = "events"

	MethodPluginList   = "plugin.list"
	MethodPluginLoad   = "plugin.load"
	MethodPluginUnload = "plugin.unload"
	MethodPluginReload = "plugin.reload"
	MethodMCPList      = "mcp.list"
	MethodMCPStart     = "mcp.start"
	MethodMCPStop      = "mcp.stop"
	MethodMCPRestart   = "mcp.restart"
	MethodMCPAdd       = "mcp.add"
	MethodMCPRemove    = "mcp.remove"

	// V2 observability streams
	MethodEventLogs     = "event.logs"
	MethodEvaluatorLogs = "evaluator.logs"
	MethodActuatorList  = "actuator.list"
	MethodActuatorRun   = "actuator.run"
	MethodActuatorLogs  = "actuator.logs"

	// V2 HITL approvals
	MethodApprovalList   = "approval.list"
	MethodApprovalShow   = "approval.show"
	MethodApprovalDecide = "approval.decide"
	MethodApprovalSubmit = "approval.submit"

	// V2 natural-language request
	MethodRequest = "request"

	// V2 automations discovery
	MethodAutomationsList = "automations.list"
)

// StreamEntry is a single line-delimited JSON entry written on a streaming
// socket connection (event.logs, evaluator.logs, actuator.logs).
type StreamEntry struct {
	Stream    string `json:"stream"`
	Time      string `json:"time,omitempty"`
	Type      string `json:"type,omitempty"`   // event type (event stream)
	Source    string `json:"source,omitempty"` // actuator id or sensor name
	Level     string `json:"level,omitempty"`  // log level (evaluator/actuator)
	Msg       string `json:"msg,omitempty"`
	Data      any    `json:"data,omitempty"`
	ActuatorID string `json:"id,omitempty"` // actuator stream
}

// StreamRequest wraps a Request with streaming-specific params.
type StreamRequest struct {
	Method string `json:"method"`
	// Tail is the number of historical entries to replay (-1 = all).
	Tail   int    `json:"tail"`
	// Follow keeps the connection open for real-time entries.
	Follow bool   `json:"follow"`
	// Filter params (event stream only)
	FilterType   string `json:"filter_type,omitempty"`
	FilterSource string `json:"filter_source,omitempty"`
	// Target actuator (actuator.logs, actuator.run)
	ActuatorID string `json:"actuator_id,omitempty"`
	// Payload for actuator.run
	Payload any `json:"payload,omitempty"`
}
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
