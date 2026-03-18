package interpreter_test

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/rs/zerolog"
)

func TestStarlarkInterpreter_ReplaysToolCalls(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	calls := 0
	reg.Register("echo", func(_ context.Context, args map[string]any) (any, error) {
		calls++
		return map[string]any{"value": args["value"]}, nil
	})

	var (
		history []interpreter.ReplayEntry
		steps   []interpreter.StepEvent
	)
	it := interpreter.NewStarlark(reg, zerolog.Nop()).
		WithHistory(
			func(_ context.Context, _ string) ([]interpreter.ReplayEntry, error) {
				return append([]interpreter.ReplayEntry(nil), history...), nil
			},
			func(_ context.Context, _, _ string, entry interpreter.ReplayEntry) error {
				history = append(history, entry)
				return nil
			},
		).
		WithOnStep(func(_ context.Context, event interpreter.StepEvent) {
			steps = append(steps, event)
		})

	intent := &orchestrator.Intent{
		ID:           "script",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script: `
init(id = "script", description = "test")

def main():
    return tool("echo", value = "hello")
`,
	}

	if err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "run-1"}); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "run-1"}); err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	if calls != 1 {
		t.Fatalf("tool should be called once, got %d", calls)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if len(steps) != 2 {
		t.Fatalf("expected replay to emit second step event, got %d", len(steps))
	}
}

func TestStarlarkInterpreter_WaitReturnsPauseError(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	it := interpreter.NewStarlark(reg, zerolog.Nop())

	intent := &orchestrator.Intent{
		ID:           "pause-script",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script: `
init(id = "pause-script")

def main():
    return wait("approval", prompt = "Continue?")
`,
	}

	err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "run-2"})
	var pauseErr *interpreter.PauseError
	if err == nil || !stderrors.As(err, &pauseErr) {
		t.Fatalf("expected PauseError, got %v", err)
	}
	if pauseErr.Request.Name != "approval" {
		t.Fatalf("unexpected pause request: %#v", pauseErr.Request)
	}
}

func TestStarlarkInterpreter_ReplaysWaitResults(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	waitCalls := 0
	var history []interpreter.ReplayEntry

	it := interpreter.NewStarlark(reg, zerolog.Nop()).
		WithWait(func(_ context.Context, stateName string, mem map[string]any) (any, error) {
			waitCalls++
			return map[string]any{"approved": true, "name": stateName, "args": mem["args"]}, nil
		}).
		WithHistory(
			func(_ context.Context, _ string) ([]interpreter.ReplayEntry, error) {
				return append([]interpreter.ReplayEntry(nil), history...), nil
			},
			func(_ context.Context, _, _ string, entry interpreter.ReplayEntry) error {
				history = append(history, entry)
				return nil
			},
		)

	intent := &orchestrator.Intent{
		ID:           "wait-script",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script: `
init(id = "wait-script")

def main():
    return wait("approval", prompt = "Ship it?")
`,
	}

	if err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "run-3"}); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "run-3"}); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if waitCalls != 1 {
		t.Fatalf("wait should be called once, got %d", waitCalls)
	}
}

func TestStarlarkInterpreter_EntrypointReceivesArgs(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	it := interpreter.NewStarlark(reg, zerolog.Nop())

	intent := &orchestrator.Intent{
		ID:           "entrypoint-script",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script: `
init(id = "entrypoint-script")

def main():
    return "main"

def on_change(event):
    return event["kind"] + ":" + event["value"]
`,
	}

	if err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{
		RunID:      "run-entrypoint",
		Entrypoint: "on_change",
		HandlerArgs: map[string]any{
			"kind":  "reminder",
			"value": "updated",
		},
	}); err != nil {
		t.Fatalf("Execute entrypoint failed: %v", err)
	}
}
