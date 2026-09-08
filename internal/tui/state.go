package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/brightpuddle/clara/pkg/sdk"
)

// Panel identifies the currently focused UI panel.
type Panel int

const (
	PanelEvents Panel = iota
	PanelRules
	PanelActuators
	PanelApprovals
	PanelInspector
)

func (p Panel) String() string {
	switch p {
	case PanelEvents:
		return "1: Events"
	case PanelRules:
		return "2: Evaluator / Rules"
	case PanelActuators:
		return "3: Actuators / Tools"
	case PanelApprovals:
		return "4: Approvals"
	case PanelInspector:
		return "5: Inspector"
	default:
		return "Unknown"
	}
}

// EventItem represents an ingress CloudEvent and its correlated lifecycle trace.
type EventItem struct {
	ID         string         `json:"id"`
	Time       time.Time      `json:"time"`
	Type       string         `json:"type"`
	Source     string         `json:"source"`
	Status     string         `json:"status"` // "success", "error", "blocked", "skipped", "pending"
	ActuatorID string         `json:"actuator_id,omitempty"`
	RuleID     string         `json:"rule_id,omitempty"`
	Routing    string         `json:"routing,omitempty"` // "fast-path", "llm-dynamic", "builder"
	Latency    time.Duration  `json:"latency,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	Logs       []string       `json:"logs,omitempty"`
	RawJSON    string         `json:"raw_json,omitempty"`
}

// RuleItem represents a configured fast-path heuristic or discovered automation.
type RuleItem struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	EventType     string           `json:"event_type"`
	SourcePattern string           `json:"source_pattern,omitempty"`
	PayloadMatch  string           `json:"payload_match,omitempty"`
	ActuatorID    string           `json:"actuator_id"`
	Routing       string           `json:"routing"` // "fast-path" or "llm-dynamic"
	TTL           string           `json:"ttl,omitempty"`
	ExpiresIn     string           `json:"expires_in,omitempty"`
	Description   string           `json:"description,omitempty"`
	Triggers      []string         `json:"triggers,omitempty"`
	Capabilities  []sdk.Capability `json:"capabilities,omitempty"`
	HitCount      int              `json:"hit_count"`
	LastMatched   time.Time        `json:"last_matched,omitempty"`
}

// ActuatorItem represents an actuator binary and its health/telemetry.
type ActuatorItem struct {
	ID           string           `json:"id"`
	Description  string           `json:"description"`
	Status       string           `json:"status"` // "healthy", "error", "idle"
	Capabilities []sdk.Capability `json:"capabilities,omitempty"`
	ExecCount    int              `json:"exec_count"`
	FailCount    int              `json:"fail_count"`
	LastRun      time.Time        `json:"last_run,omitempty"`
	LastOutput   string           `json:"last_output,omitempty"`
	LastError    string           `json:"last_error,omitempty"`
}

// ApprovalItem represents an active HITL decision request.
type ApprovalItem struct {
	RequestID string `json:"request_id"`
	Context   string `json:"context"`
	CreatedAt time.Time
	Options   []ResolutionOption `json:"options"`
}

// ResolutionOption represents a selectable option in an approval request.
type ResolutionOption struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	ActionCode  string `json:"action_code"`
}

// AppState manages all TUI list data and navigation pointers.
type AppState struct {
	Events    []EventItem
	Rules     []RuleItem
	Actuators []ActuatorItem
	Approvals []ApprovalItem
	Tools     []ToolData

	// Navigation indexes per panel
	EventIdx    int
	RuleIdx     int
	ActuatorIdx int
	ApprovalIdx int

	// Trace index inside the selected event
	TraceScroll int

	// Search filter per panel
	FilterActive bool
	FilterText   string

	// Track seen event IDs to prevent duplicate listings
	seenEvents map[string]int // id -> index in Events
}

// NewAppState initializes an empty AppState.
func NewAppState() *AppState {
	return &AppState{
		Events:     make([]EventItem, 0),
		Rules:      make([]RuleItem, 0),
		Actuators:  make([]ActuatorItem, 0),
		Approvals:  make([]ApprovalItem, 0),
		Tools:      make([]ToolData, 0),
		seenEvents: make(map[string]int),
	}
}

// AddOrUpdateEvent inserts or updates an event in state.
func (s *AppState) AddOrUpdateEvent(item EventItem) {
	if item.ID == "" {
		item.ID = fmt.Sprintf("ev-%d", time.Now().UnixNano())
	}
	if idx, exists := s.seenEvents[item.ID]; exists && idx < len(s.Events) {
		// Update existing event trace
		existing := &s.Events[idx]
		if item.Status != "" {
			existing.Status = item.Status
		}
		if item.ActuatorID != "" {
			existing.ActuatorID = item.ActuatorID
		}
		if item.RuleID != "" {
			existing.RuleID = item.RuleID
		}
		if item.Routing != "" {
			existing.Routing = item.Routing
		}
		if len(item.Logs) > 0 {
			existing.Logs = append(existing.Logs, item.Logs...)
		}
		return
	}

	if item.Status == "" {
		item.Status = "pending"
	}
	if item.Time.IsZero() {
		item.Time = time.Now()
	}

	// Insert at the front (newest first)
	s.Events = append([]EventItem{item}, s.Events...)
	// Cap to 200 items
	if len(s.Events) > 200 {
		s.Events = s.Events[:200]
	}

	// Rebuild index map
	s.seenEvents = make(map[string]int, len(s.Events))
	for i, ev := range s.Events {
		s.seenEvents[ev.ID] = i
	}
}

// AppendEventLog adds a log line to a specific event if found, or the latest event.
func (s *AppState) AppendEventLog(eventID, logLine string) {
	if eventID != "" {
		if idx, ok := s.seenEvents[eventID]; ok && idx < len(s.Events) {
			s.Events[idx].Logs = append(s.Events[idx].Logs, logLine)
			return
		}
	}
	if len(s.Events) > 0 {
		s.Events[0].Logs = append(s.Events[0].Logs, logLine)
	}
}

// RecordActuatorRun updates run counters and status for an actuator.
func (s *AppState) RecordActuatorRun(actuatorID string, success bool, output, errText string) {
	for i := range s.Actuators {
		if s.Actuators[i].ID == actuatorID {
			s.Actuators[i].ExecCount++
			s.Actuators[i].LastRun = time.Now()
			s.Actuators[i].LastOutput = output
			s.Actuators[i].LastError = errText
			if success {
				s.Actuators[i].Status = "healthy"
			} else {
				s.Actuators[i].FailCount++
				s.Actuators[i].Status = "error"
			}
			return
		}
	}
	// If not found in list, append it
	status := "healthy"
	failCount := 0
	if !success {
		status = "error"
		failCount = 1
	}
	s.Actuators = append(s.Actuators, ActuatorItem{
		ID:         actuatorID,
		Status:     status,
		ExecCount:  1,
		FailCount:  failCount,
		LastRun:    time.Now(),
		LastOutput: output,
		LastError:  errText,
	})
}

// FilteredEvents returns events matching the filter query.
func (s *AppState) FilteredEvents() []EventItem {
	if s.FilterText == "" {
		return s.Events
	}
	q := strings.ToLower(s.FilterText)
	var filtered []EventItem
	for _, ev := range s.Events {
		if strings.Contains(strings.ToLower(ev.ID), q) ||
			strings.Contains(strings.ToLower(ev.Type), q) ||
			strings.Contains(strings.ToLower(ev.Source), q) ||
			strings.Contains(strings.ToLower(ev.ActuatorID), q) ||
			strings.Contains(strings.ToLower(ev.Status), q) {
			filtered = append(filtered, ev)
		}
	}
	return filtered
}

// FilteredRules returns rules matching the filter query.
func (s *AppState) FilteredRules() []RuleItem {
	if s.FilterText == "" {
		return s.Rules
	}
	q := strings.ToLower(s.FilterText)
	var filtered []RuleItem
	for _, r := range s.Rules {
		if strings.Contains(strings.ToLower(r.ID), q) ||
			strings.Contains(strings.ToLower(r.EventType), q) ||
			strings.Contains(strings.ToLower(r.ActuatorID), q) ||
			strings.Contains(strings.ToLower(r.Description), q) ||
			strings.Contains(strings.ToLower(r.Routing), q) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// FilteredActuators returns actuators matching the filter query.
func (s *AppState) FilteredActuators() []ActuatorItem {
	if s.FilterText == "" {
		return s.Actuators
	}
	q := strings.ToLower(s.FilterText)
	var filtered []ActuatorItem
	for _, a := range s.Actuators {
		if strings.Contains(strings.ToLower(a.ID), q) ||
			strings.Contains(strings.ToLower(a.Description), q) ||
			strings.Contains(strings.ToLower(a.Status), q) {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

// PrettyJSON formats data as indented JSON.
func PrettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
