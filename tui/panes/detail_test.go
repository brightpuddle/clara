package panes

import (
	"testing"
	"time"

	agentv1 "github.com/brightpuddle/clara/gen/agent/v1"
	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
)

func TestDetailPane_SetArtifact(t *testing.T) {
	p := NewDetailPane()
	p.SetSize(80, 24)

	a := &artifactv1.Artifact{
		Id:    "d-1",
		Title: "Test Note",
		Kind:  artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE,
	}
	p.SetArtifact(a)

	if p.artifact == nil || p.artifact.Id != "d-1" {
		t.Error("expected artifact to be set")
	}
	if p.settingsCategory != "" {
		t.Error("expected settings category to be cleared after SetArtifact")
	}
	if p.scrollY != 0 {
		t.Error("expected scroll to reset to 0")
	}
}

func TestDetailPane_SetSettingsView_Status(t *testing.T) {
	p := NewDetailPane()
	p.SetSize(80, 24)

	before := time.Now()
	p.SetSettingsView("status", &agentv1.GetStatusResponse{
		Agent: &agentv1.ComponentStatus{Connected: true, State: "running", UptimeSeconds: 120},
	}, nil)
	after := time.Now()

	if p.settingsCategory != "status" {
		t.Errorf("settingsCategory = %q, want %q", p.settingsCategory, "status")
	}
	if p.statusData == nil {
		t.Fatal("expected statusData to be set")
	}
	if p.statusFetchedAt.Before(before) || p.statusFetchedAt.After(after) {
		t.Error("statusFetchedAt not set to approximately now")
	}
	if p.uptimeTick != 0 {
		t.Error("expected uptimeTick reset to 0 on new status data")
	}
}

func TestDetailPane_SetSettingsView_NilStatusData(t *testing.T) {
	p := NewDetailPane()
	p.SetSettingsView("status", &agentv1.GetStatusResponse{}, nil)
	p.TickUptime() // tick once to set uptimeTick = 1

	// Re-set with new status data → should reset uptimeTick
	p.SetSettingsView("status", &agentv1.GetStatusResponse{}, nil)
	if p.uptimeTick != 0 {
		t.Error("expected uptimeTick reset to 0 on new data")
	}
}

func TestDetailPane_TickUptime(t *testing.T) {
	p := NewDetailPane()
	p.SetSettingsView("status", &agentv1.GetStatusResponse{
		Agent: &agentv1.ComponentStatus{Connected: true, UptimeSeconds: 100},
	}, nil)

	for i := 0; i < 5; i++ {
		p.TickUptime()
	}

	if p.uptimeTick != 5 {
		t.Errorf("uptimeTick = %d, want 5", p.uptimeTick)
	}
}

func TestDetailPane_TickUptime_NoDataNoOp(t *testing.T) {
	p := NewDetailPane()
	// No status data set — tick should be no-op
	p.TickUptime()
	if p.uptimeTick != 0 {
		t.Error("expected uptimeTick to remain 0 without status data")
	}
}

func TestDetailPane_Scroll(t *testing.T) {
	p := NewDetailPane()
	p.SetSize(80, 24)

	p.ScrollDown()
	p.ScrollDown()
	if p.scrollY != 2 {
		t.Errorf("scrollY = %d, want 2", p.scrollY)
	}

	p.ScrollUp()
	if p.scrollY != 1 {
		t.Errorf("scrollY = %d, want 1", p.scrollY)
	}

	p.ScrollUp()
	p.ScrollUp() // clamped at 0
	if p.scrollY != 0 {
		t.Errorf("scrollY = %d, want 0 (clamped)", p.scrollY)
	}
}

func TestDetailPane_View_Empty(t *testing.T) {
	p := NewDetailPane()
	p.SetSize(80, 24)
	// Should not panic on empty artifact
	_ = p.View()
}

func TestDetailPane_View_WithArtifact(t *testing.T) {
	p := NewDetailPane()
	p.SetSize(80, 24)
	a := &artifactv1.Artifact{
		Id:    "v-1",
		Title: "View Test",
		Kind:  artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE,
	}
	p.SetArtifact(a)
	out := p.View()
	if out == "" {
		t.Error("expected non-empty view with artifact")
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		seconds int64
		want    string
	}{
		{0, "0s"},
		{45, "45s"},
		{60, "1m0s"},
		{90, "1m30s"},
		{3600, "1h0m"},
		{3661, "1h1m"},
		{7320, "2h2m"},
	}
	for _, tc := range tests {
		got := formatUptime(tc.seconds)
		if got != tc.want {
			t.Errorf("formatUptime(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}
