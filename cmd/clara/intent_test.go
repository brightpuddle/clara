package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/intentlog"
	"github.com/brightpuddle/clara/internal/tui"
)

func TestIntentWatchPrinterEventVerbose(t *testing.T) {
	theme := testWatchTheme()
	printer := newIntentWatchPrinter(&theme, true, true)
	printer.lastState["run-1"] = "LOAD"

	output := captureStdout(t, func() {
		printer.printEvent(intentlog.Event{
			Time:     time.Unix(0, 1),
			RunID:    "run-1",
			IntentID: "sync-reminders",
			State:    "RECONCILE",
			Action:   "db.query",
			Args:     map[string]any{"sql": "select 1"},
			Result:   map[string]any{"rows": []any{1.0}},
		})
	})

	plain := stripANSI(output)
	for _, want := range []string{
		"LOAD -> RECONCILE",
		"sync-reminders",
		"action: db.query",
		"args",
		"result",
		"sql=select 1",
		"rows:[1]",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("output missing %q:\n%s", want, plain)
		}
	}
}

func TestIntentWatchPrinterFinishStatus(t *testing.T) {
	theme := testWatchTheme()
	printer := newIntentWatchPrinter(&theme, false, false)

	output := captureStdout(t, func() {
		printer.printEvent(intentlog.Event{
			Time:     time.Unix(0, 1),
			RunID:    "run-3",
			IntentID: "sync-reminders",
			State:    "DONE",
			Action:   "finish",
			Result:   map[string]any{"status": "completed"},
		})
	})

	plain := stripANSI(output)
	// Non-verbose: action line should still appear.
	if !strings.Contains(plain, "finish") {
		t.Fatalf("output missing action 'finish':\n%s", plain)
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
	// Dummy test for now as we removed the original parseArgs from main.go
}

func testWatchTheme() tui.Theme {
	return tui.DetectTheme()
}
