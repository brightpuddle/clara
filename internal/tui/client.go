package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/pkg/sdk"
)

// DaemonSnapshot aggregates current state from the running Clara daemon.
type DaemonSnapshot struct {
	Online      bool
	StatusMsg   string
	Automations []AutomationData
	Actuators   []ActuatorData
	Approvals   []ApprovalData
	Tools       []ToolData
}

// AutomationData mirrors the supervisor.AutomationSummary schema.
type AutomationData struct {
	ActuatorID   string           `json:"actuator_id"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Triggers     []string         `json:"triggers"`
	Routing      string           `json:"routing"` // "fast-path" or "llm-dynamic"
	RuleID       string           `json:"rule_id,omitempty"`
	TTL          string           `json:"ttl,omitempty"`
	ExpiresIn    string           `json:"expires_in,omitempty"`
	Capabilities []sdk.Capability `json:"capabilities"`
}

// ActuatorData mirrors loaded actuator binary status.
type ActuatorData struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// ApprovalData represents a pending HITL approval request.
type ApprovalData struct {
	RequestID string `json:"request_id"`
	Context   string `json:"context"`
	Options   []struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		ActionCode  string `json:"action_code"`
	} `json:"options"`
}

// ToolData represents a tool or sensor in the registry.
type ToolData struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CheckDaemonOnline probes the daemon control socket.
func CheckDaemonOnline(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// SendIPCRequest sends a request and decodes the response.
func SendIPCRequest(socketPath string, req ipc.Request) (*ipc.Response, error) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp ipc.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

// FetchSnapshot retrieves a full snapshot of current daemon state.
func FetchSnapshot(socketPath string) (*DaemonSnapshot, error) {
	snap := &DaemonSnapshot{Online: false}

	if !CheckDaemonOnline(socketPath) {
		snap.StatusMsg = "Daemon offline (not reachable at " + socketPath + ")"
		return snap, nil
	}

	snap.Online = true
	snap.StatusMsg = "Connected"

	// Fetch automations
	if resp, err := SendIPCRequest(socketPath, ipc.Request{Method: ipc.MethodAutomationsList}); err == nil && resp.Error == "" {
		if raw, err := json.Marshal(resp.Data); err == nil {
			var auts []AutomationData
			_ = json.Unmarshal(raw, &auts)
			snap.Automations = auts
		}
	}

	// Fetch actuators
	if resp, err := SendIPCRequest(socketPath, ipc.Request{Method: ipc.MethodActuatorList}); err == nil && resp.Error == "" {
		if raw, err := json.Marshal(resp.Data); err == nil {
			var acts []ActuatorData
			_ = json.Unmarshal(raw, &acts)
			snap.Actuators = acts
		}
	}

	// Fetch approvals
	if resp, err := SendIPCRequest(socketPath, ipc.Request{Method: ipc.MethodApprovalList}); err == nil && resp.Error == "" {
		if raw, err := json.Marshal(resp.Data); err == nil {
			var apps []ApprovalData
			_ = json.Unmarshal(raw, &apps)
			snap.Approvals = apps
		}
	}

	// Fetch tools
	if resp, err := SendIPCRequest(socketPath, ipc.Request{Method: ipc.MethodToolList}); err == nil && resp.Error == "" {
		if raw, err := json.Marshal(resp.Data); err == nil {
			var tools []ToolData
			_ = json.Unmarshal(raw, &tools)
			snap.Tools = tools
		}
	}

	return snap, nil
}

// StreamLogs connects to an IPC stream and yields StreamEntry items.
func StreamLogs(ctx context.Context, socketPath, method string, tail int, ch chan<- ipc.StreamEntry) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	req := ipc.StreamRequest{
		Method: method,
		Tail:   tail,
		Follow: true,
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return
	}

	dec := json.NewDecoder(conn)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var entry ipc.StreamEntry
			if err := dec.Decode(&entry); err != nil {
				return
			}
			if entry.Stream == "" {
				switch method {
				case ipc.MethodEventLogs:
					entry.Stream = "event"
				case ipc.MethodEvaluatorLogs:
					entry.Stream = "evaluator"
				case ipc.MethodActuatorLogs:
					entry.Stream = "actuator"
				}
			}
			select {
			case <-ctx.Done():
				return
			case ch <- entry:
			}
		}
	}
}

// DecideApproval submits a decision for a HITL request (1-based option index).
func DecideApproval(socketPath, requestID string, optionIndex int) error {
	resp, err := SendIPCRequest(socketPath, ipc.Request{
		Method: ipc.MethodApprovalDecide,
		Params: map[string]any{
			"id":     requestID,
			"option": optionIndex,
		},
	})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}
