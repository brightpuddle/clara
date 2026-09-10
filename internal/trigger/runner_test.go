package trigger_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/trigger"
)

func TestRunner_Execute_Success(t *testing.T) {
	runner := trigger.NewRunner(5 * time.Second)

	def := trigger.Definition{
		ID:   "test-echo",
		Name: "Test Echo",
		Type: trigger.TypeEvent,
		Action: trigger.Action{
			Exec:      "sh",
			Args:      []string{"-c", "read line; echo \"Received: $line\""},
			PassEvent: trigger.PassEventStdin,
		},
	}

	eventData := map[string]any{"msg": "hello world"}
	record := runner.Execute(context.Background(), def, eventData)

	if record.Status != trigger.StatusSuccess {
		t.Fatalf("expected status success, got %v with error: %v", record.Status, record.Error)
	}
	if record.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", record.ExitCode)
	}
	if record.Stdout != "Received: {\"msg\":\"hello world\"}\n" {
		t.Errorf("unexpected stdout: %q", record.Stdout)
	}
}

func TestRunner_Execute_Failure_LetItFail(t *testing.T) {
	runner := trigger.NewRunner(5 * time.Second)

	def := trigger.Definition{
		ID:   "test-fail",
		Name: "Test Fail",
		Type: trigger.TypeEvent,
		Action: trigger.Action{
			Exec: "sh",
			Args: []string{"-c", "echo 'some error' >&2; exit 42"},
		},
	}

	record := runner.Execute(context.Background(), def, nil)

	if record.Status != trigger.StatusFailure {
		t.Fatalf("expected status failure, got %v", record.Status)
	}
	if record.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", record.ExitCode)
	}
	if record.Stderr != "some error\n" {
		t.Errorf("expected stderr 'some error\\n', got %q", record.Stderr)
	}
}

func TestRunner_Execute_Timeout(t *testing.T) {
	runner := trigger.NewRunner(100 * time.Millisecond)

	def := trigger.Definition{
		ID:   "test-timeout",
		Name: "Test Timeout",
		Type: trigger.TypeEvent,
		Action: trigger.Action{
			Exec:    "sleep",
			Args:    []string{"2"},
			Timeout: 100 * time.Millisecond,
		},
	}

	record := runner.Execute(context.Background(), def, nil)

	if record.Status != trigger.StatusTimeout {
		t.Fatalf("expected status timeout, got %v", record.Status)
	}
}

func TestRunner_Execute_EnvPassing(t *testing.T) {
	runner := trigger.NewRunner(5 * time.Second)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test.sh")
	err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho \"EVENT=$CLARA_EVENT_JSON\"\n"), 0755)
	if err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	def := trigger.Definition{
		ID:   "test-env",
		Type: trigger.TypeEvent,
		Action: trigger.Action{
			Exec:      scriptPath,
			PassEvent: trigger.PassEventEnv,
		},
	}

	eventData := map[string]any{"user": "nathan"}
	record := runner.Execute(context.Background(), def, eventData)

	if record.Status != trigger.StatusSuccess {
		t.Fatalf("expected success, got %v (err: %v)", record.Status, record.Error)
	}
	if record.Stdout != "EVENT={\"user\":\"nathan\"}\n" {
		t.Errorf("unexpected stdout: %q", record.Stdout)
	}
}
