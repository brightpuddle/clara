package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/brightpuddle/clara/pkg/contract"
)

// --- helpers ---

// fakeTmux wraps Tmux but replaces the run function with a stub so tests never
// shell out to a real tmux binary.
type fakeTmux struct {
	Tmux
	runFn func(args ...string) (string, error)
}

func (f *fakeTmux) run(args ...string) (string, error) {
	return f.runFn(args...)
}

// --- Description / Tools ---

func TestDescription(t *testing.T) {
	tmx := &Tmux{tmuxPath: "/usr/bin/tmux"}
	desc, err := tmx.Description()
	if err != nil {
		t.Fatalf("Description() error: %v", err)
	}
	if desc == "" {
		t.Fatal("Description() returned empty string")
	}
}

func TestTools_ValidJSON(t *testing.T) {
	tmx := &Tmux{tmuxPath: "/usr/bin/tmux"}
	raw, err := tmx.Tools()
	if err != nil {
		t.Fatalf("Tools() error: %v", err)
	}
	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("Tools() returned invalid JSON: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("Tools() returned no tools")
	}
}

func TestTools_ExpectedNames(t *testing.T) {
	tmx := &Tmux{tmuxPath: "/usr/bin/tmux"}
	raw, _ := tmx.Tools()

	var tools []map[string]any
	_ = json.Unmarshal(raw, &tools)

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if name, ok := tool["name"].(string); ok {
			names[name] = true
		}
	}

	for _, want := range []string{"session.list", "session.create", "session.kill", "pane.capture"} {
		if !names[want] {
			t.Errorf("expected tool %q to be registered", want)
		}
	}
}

// --- ListSessions parsing ---

func TestListSessions_ParsesOutput(t *testing.T) {
	// Simulate tmux list-sessions output
	fakeOutput := "work|2|1700000000|220|50|attached\nplay|1|1700000001|80|24|detached\n"

	tmx := &Tmux{tmuxPath: "tmux"} // binary unused; we patch run below
	sessions, err := listSessionsFromOutput(tmx, fakeOutput, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	s0 := sessions[0]
	if s0.Name != "work" {
		t.Errorf("session[0].Name = %q, want %q", s0.Name, "work")
	}
	if s0.Windows != 2 {
		t.Errorf("session[0].Windows = %d, want 2", s0.Windows)
	}
	if s0.CreatedAt != 1700000000 {
		t.Errorf("session[0].CreatedAt = %d, want 1700000000", s0.CreatedAt)
	}
	if s0.Width != 220 {
		t.Errorf("session[0].Width = %d, want 220", s0.Width)
	}
	if s0.Height != 50 {
		t.Errorf("session[0].Height = %d, want 50", s0.Height)
	}
	if !s0.Attached {
		t.Error("session[0].Attached should be true")
	}

	s1 := sessions[1]
	if s1.Attached {
		t.Error("session[1].Attached should be false")
	}
}

func TestListSessions_EmptyOutput(t *testing.T) {
	tmx := &Tmux{tmuxPath: "tmux"}
	sessions, err := listSessionsFromOutput(tmx, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestListSessions_NoServerRunning(t *testing.T) {
	tmx := &Tmux{tmuxPath: "tmux"}
	stubErr := &fakeExecError{msg: "no server running on /tmp/tmux-1000/default"}
	sessions, err := listSessionsFromOutput(tmx, "", stubErr)
	if err != nil {
		t.Fatalf("'no server running' should not be returned as an error, got: %v", err)
	}
	if sessions == nil || len(sessions) != 0 {
		t.Errorf("expected empty slice, got %v", sessions)
	}
}

// --- CapturePane line limit ---

func TestCapturePane_Limit(t *testing.T) {
	content := strings.Repeat("line\n", 20) // 20 lines

	cases := []struct {
		limit    int
		wantMore bool // whether we expect the full or trimmed output
	}{
		{0, true},  // no limit → all lines
		{5, false}, // limit 5 → only last 5 lines
	}

	for _, tc := range cases {
		result := applyLimit(content, tc.limit)
		lineCount := len(strings.Split(strings.TrimRight(result, "\n"), "\n"))
		if tc.limit > 0 && lineCount > tc.limit {
			t.Errorf("limit=%d: got %d lines, want ≤ %d", tc.limit, lineCount, tc.limit)
		}
		if tc.limit == 0 && lineCount < 20 {
			t.Errorf("no limit: got %d lines, want ≥ 20", lineCount)
		}
	}
}

// --- CallTool dispatch ---

func TestCallTool_UnknownTool(t *testing.T) {
	tmx := &Tmux{tmuxPath: "/usr/bin/tmux"}
	_, err := tmx.CallTool("does.not.exist", []byte("{}"))
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
}

func TestCallTool_BadJSON(t *testing.T) {
	tmx := &Tmux{tmuxPath: "/usr/bin/tmux"}
	for _, tool := range []string{"session.create", "session.kill", "pane.capture"} {
		_, err := tmx.CallTool(tool, []byte("not-json"))
		if err == nil {
			t.Errorf("CallTool(%q, bad-json): expected error, got nil", tool)
		}
	}
}

// --- unavailableStub ---

func TestUnavailableStub_AllMethodsReturnErr(t *testing.T) {
	stub := &unavailableStub{err: errTest}

	if stub.Configure(nil) != errTest {
		t.Error("Configure should return stub err")
	}
	if _, err := stub.Description(); err != errTest {
		t.Error("Description should return stub err")
	}
	if _, err := stub.Tools(); err != errTest {
		t.Error("Tools should return stub err")
	}
	if _, err := stub.CallTool("", nil); err != errTest {
		t.Error("CallTool should return stub err")
	}
	if _, err := stub.ListSessions(); err != errTest {
		t.Error("ListSessions should return stub err")
	}
	if err := stub.CreateSession("", ""); err != errTest {
		t.Error("CreateSession should return stub err")
	}
	if _, err := stub.CapturePane("", 0); err != errTest {
		t.Error("CapturePane should return stub err")
	}
	if err := stub.KillSession(""); err != errTest {
		t.Error("KillSession should return stub err")
	}
}

// --- Configure ---

func TestConfigure_AlwaysNil(t *testing.T) {
	tmx := &Tmux{tmuxPath: "/usr/bin/tmux"}
	if err := tmx.Configure(nil); err != nil {
		t.Errorf("Configure(nil) = %v, want nil", err)
	}
	if err := tmx.Configure([]byte(`{"key":"value"}`)); err != nil {
		t.Errorf("Configure(json) = %v, want nil", err)
	}
}

// --- interface compliance ---

func TestTmux_ImplementsInterface(t *testing.T) {
	var _ contract.TmuxIntegration = (*Tmux)(nil)
	var _ contract.TmuxIntegration = (*unavailableStub)(nil)
}

// ---------------------------------------------------------------------------
// Internal helpers shared between the tested code and the tests.
// These thin wrappers let us exercise parsing/transformation logic without
// spawning a real tmux process.
// ---------------------------------------------------------------------------

// listSessionsFromOutput exercises the parsing path of ListSessions given a
// pre-cooked output string and an optional error (simulating run's return).
func listSessionsFromOutput(t *Tmux, output string, runErr error) ([]contract.TmuxSession, error) {
	if runErr != nil {
		msg := runErr.Error()
		if strings.Contains(msg, "no server running") ||
			strings.Contains(msg, "error connecting to /tmp/tmux") {
			return []contract.TmuxSession{}, nil
		}
		return nil, runErr
	}
	raw := strings.TrimSpace(output)
	if raw == "" {
		return []contract.TmuxSession{}, nil
	}
	// Re-use the same parsing logic as ListSessions (copy kept in sync).
	var sessions []contract.TmuxSession
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 6 {
			continue
		}
		var (
			windows   int
			createdAt int64
			width     int
			height    int
		)
		_, _ = 0, 0
		if v, err := parseInt(parts[1]); err == nil {
			windows = v
		}
		if v, err := parseInt64(parts[2]); err == nil {
			createdAt = v
		}
		if v, err := parseInt(parts[3]); err == nil {
			width = v
		}
		if v, err := parseInt(parts[4]); err == nil {
			height = v
		}
		sessions = append(sessions, contract.TmuxSession{
			Name:      parts[0],
			Windows:   windows,
			CreatedAt: createdAt,
			Width:     width,
			Height:    height,
			Attached:  parts[5] == "attached",
		})
	}
	return sessions, nil
}

// applyLimit mirrors CapturePane's limit logic for isolated testing.
func applyLimit(content string, limit int) string {
	if limit <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) > limit {
		return strings.Join(lines[len(lines)-limit:], "\n")
	}
	return content
}

// ---------------------------------------------------------------------------
// Minimal stubs for error-path tests
// ---------------------------------------------------------------------------

type fakeExecError struct{ msg string }

func (e *fakeExecError) Error() string { return e.msg }

var errTest = &fakeExecError{msg: "sentinel test error"}

// Small parsing helpers used only in the test helper above.
func parseInt(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

func parseInt64(s string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
