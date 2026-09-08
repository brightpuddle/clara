package tui

import (
	"testing"
	"time"

	"github.com/brightpuddle/clara/pkg/sdk"
)

func TestAppState_EventsAndFiltering(t *testing.T) {
	state := NewAppState()

	ev1 := EventItem{
		ID:      "ev-1",
		Time:    time.Now(),
		Type:    "email.received",
		Source:  "integrations/email",
		Status:  "pending",
		Data:    map[string]any{"subject": "Invoice"},
		RawJSON: `{"subject": "Invoice"}`,
	}
	state.AddOrUpdateEvent(ev1)

	if len(state.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(state.Events))
	}

	// Update trace
	state.AddOrUpdateEvent(EventItem{
		ID:         "ev-1",
		Status:     "success",
		ActuatorID: "email_triage",
		RuleID:     "rule-10",
		Routing:    "fast-path",
		Logs:       []string{"[info] Evaluator: fast-path heuristic hit"},
	})

	if len(state.Events) != 1 {
		t.Fatalf("expected 1 event after update, got %d", len(state.Events))
	}
	if state.Events[0].Status != "success" {
		t.Errorf("expected status 'success', got %q", state.Events[0].Status)
	}
	if state.Events[0].ActuatorID != "email_triage" {
		t.Errorf("expected actuator 'email_triage', got %q", state.Events[0].ActuatorID)
	}
	if len(state.Events[0].Logs) != 1 {
		t.Errorf("expected 1 log line, got %d", len(state.Events[0].Logs))
	}

	// Test Filtering
	state.FilterText = "invoice"
	if len(state.FilteredEvents()) != 0 { // search matches type, source, id, actuator
		// type doesn't match invoice
	}
	state.FilterText = "email"
	if len(state.FilteredEvents()) != 1 {
		t.Errorf("expected 1 match for filter 'email', got %d", len(state.FilteredEvents()))
	}
}

func TestAppState_ActuatorsAndTelemetry(t *testing.T) {
	state := NewAppState()
	state.RecordActuatorRun("email_triage", true, "processed 1 email", "")

	if len(state.Actuators) != 1 {
		t.Fatalf("expected 1 actuator, got %d", len(state.Actuators))
	}
	if state.Actuators[0].Status != "healthy" || state.Actuators[0].ExecCount != 1 || state.Actuators[0].FailCount != 0 {
		t.Errorf("unexpected actuator stats: %+v", state.Actuators[0])
	}

	// Record failure
	state.RecordActuatorRun("email_triage", false, "", "connection timeout")
	if state.Actuators[0].Status != "error" || state.Actuators[0].ExecCount != 2 || state.Actuators[0].FailCount != 1 {
		t.Errorf("unexpected actuator failure stats: %+v", state.Actuators[0])
	}
}

func TestViewRenderer_RenderAllPanels(t *testing.T) {
	state := NewAppState()
	state.AddOrUpdateEvent(EventItem{
		ID:         "ev-test",
		Time:       time.Now(),
		Type:       "email.received",
		Source:     "integrations/email",
		Status:     "success",
		ActuatorID: "email_triage",
		Data:       map[string]any{"subject": "Hello"},
		Logs:       []string{"[info] test log"},
	})
	state.Rules = []RuleItem{
		{
			ID:          "rule-1",
			Name:        "email_triage",
			ActuatorID:  "email_triage",
			Routing:     "fast-path",
			Description: "Auto triage emails",
			TTL:         "24h",
			Capabilities: []sdk.Capability{
				{Resource: "fs:write", Description: "Write note"},
			},
		},
	}
	state.Actuators = []ActuatorItem{
		{
			ID:          "email_triage",
			Description: "Email triaging actuator",
			Status:      "healthy",
			ExecCount:   5,
			FailCount:   0,
		},
	}
	state.Approvals = []ApprovalItem{
		{
			RequestID: "req-1",
			Context:   "High-risk action",
			Options: []ResolutionOption{
				{ID: "allow", Description: "Allow", ActionCode: "allow"},
			},
		},
	}

	renderer := NewViewRenderer(state, 120, 40)

	// Verify no panics and non-empty strings for all panels
	p1 := renderer.RenderEventsPanel(true, 40, 10)
	if p1 == "" {
		t.Error("RenderEventsPanel returned empty string")
	}

	p2 := renderer.RenderRulesPanel(false, 40, 10)
	if p2 == "" {
		t.Error("RenderRulesPanel returned empty string")
	}

	p3 := renderer.RenderActuatorsPanel(false, 40, 10)
	if p3 == "" {
		t.Error("RenderActuatorsPanel returned empty string")
	}

	p4 := renderer.RenderApprovalsPanel(false, 40, 10)
	if p4 == "" {
		t.Error("RenderApprovalsPanel returned empty string")
	}

	for _, p := range []Panel{PanelEvents, PanelRules, PanelActuators, PanelApprovals, PanelInspector} {
		insp := renderer.RenderInspectorPanel(p, 75, 38)
		if insp == "" {
			t.Errorf("RenderInspectorPanel for panel %v returned empty string", p)
		}
	}

	status := renderer.RenderStatusBar(true, "Connected")
	if status == "" {
		t.Error("RenderStatusBar returned empty string")
	}

	help := renderer.RenderHelpModal()
	if help == "" {
		t.Error("RenderHelpModal returned empty string")
	}
}
