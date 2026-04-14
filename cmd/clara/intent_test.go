package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/tui"
)

func TestIntentWatchPrinterEventVerbose(t *testing.T) {
	printer := newIntentWatchPrinter(testWatchTheme(), true, true)
	printer.lastState["run-1"] = "LOAD"

	output := captureStdout(t, func() {
		printer.printEvent(store.RunEvent{
			RunID:     "run-1",
			IntentID:  "sync-reminders",
			State:     "RECONCILE",
			Action:    "db.query",
			Args:      map[string]any{"sql": "select 1"},
			Result:    map[string]any{"rows": []any{1.0}},
			CreatedAt: 1,
		})
	})

	plain := stripANSI(output)
	for _, want := range []string{
		"LOAD -> RECONCILE",
		"sync-reminders",
		"action: db.query",
		"args",
		"result",
		"\"sql\": \"select 1\"",
		"\"rows\":",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("output missing %q:\n%s", want, plain)
		}
	}
}

func TestIntentWatchPrinterStateSnapshot(t *testing.T) {
	printer := newIntentWatchPrinter(testWatchTheme(), false, false)

	output := captureStdout(t, func() {
		printer.printStateSnapshot(store.RunState{
			RunID:     "run-2",
			IntentID:  "sync-reminders",
			State:     "WAIT",
			Status:    "running",
			UpdatedAt: 1,
		})
	})

	plain := stripANSI(output)
	for _, want := range []string{"WAIT", "run: run-2", "status: running"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, plain)
		}
	}
}

func TestIntentWatchPrinterFinishStatus(t *testing.T) {
	printer := newIntentWatchPrinter(testWatchTheme(), false, false)

	output := captureStdout(t, func() {
		printer.printEvent(store.RunEvent{
			RunID:     "run-3",
			IntentID:  "sync-reminders",
			State:     "DONE",
			Result:    map[string]any{"status": "completed"},
			CreatedAt: 1,
		})
	})

	plain := stripANSI(output)
	if !strings.Contains(plain, "status: completed") {
		t.Fatalf("finish event missing status line:\n%s", plain)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()

	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return buf.String()
}

func stripANSI(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(s, "")
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected map[string]any
	}{
		{
			name: "basic types",
			args: []string{"name=clara", "count=42", "pi=3.14", "active=true"},
			expected: map[string]any{
				"name":   "clara",
				"count":  int64(42),
				"pi":     3.14,
				"active": true,
			},
		},
		{
			name: "mixed args",
			args: []string{"invalid", "key=value", "foo=bar=baz"},
			expected: map[string]any{
				"key": "value",
				"foo": "bar=baz",
			},
		},
		{
			name:     "empty",
			args:     []string{},
			expected: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseArgs(tt.args)
			if len(got) != len(tt.expected) {
				t.Fatalf("got %d args, want %d", len(got), len(tt.expected))
			}
			for k, v := range tt.expected {
				if got[k] != v {
					t.Errorf("arg %q: got %v (%T), want %v (%T)", k, got[k], got[k], v, v)
				}
			}
		})
	}
}

func testWatchTheme() tui.Theme {
	return tui.DetectTheme()
}
