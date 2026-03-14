package interpreter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/rs/zerolog"
)

func newTestInterpreter(t *testing.T) (*interpreter.Interpreter, *registry.Registry) {
	t.Helper()
	reg := registry.New(zerolog.Nop())
	it := interpreter.New(reg, zerolog.Nop())
	return it, reg
}

func TestInterpreter_HappyPath(t *testing.T) {
	it, reg := newTestInterpreter(t)

	var calls []string
	reg.Register("step.a", func(_ context.Context, _ map[string]any) (any, error) {
		calls = append(calls, "a")
		return "result-a", nil
	})
	reg.Register("step.b", func(_ context.Context, _ map[string]any) (any, error) {
		calls = append(calls, "b")
		return "result-b", nil
	})

	bp := &orchestrator.Intent{
		ID:           "test",
		InitialState: "A",
		States: map[string]orchestrator.State{
			"A": {Action: "step.a", Next: "B"},
			"B": {Action: "step.b", Terminal: true},
		},
	}

	err := it.Execute(context.Background(), bp, "A", interpreter.RunOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Errorf("calls: got %v want [a b]", calls)
	}
}

func TestInterpreter_TransitionCondition(t *testing.T) {
	it, reg := newTestInterpreter(t)

	reg.Register("fetch", func(_ context.Context, _ map[string]any) (any, error) {
		return map[string]any{"count": 5}, nil
	})
	reg.Register("handle_nonempty", func(_ context.Context, _ map[string]any) (any, error) {
		return "done", nil
	})
	reg.Register("handle_empty", func(_ context.Context, _ map[string]any) (any, error) {
		return "empty", nil
	})

	bp := &orchestrator.Intent{
		ID:           "transition-test",
		InitialState: "FETCH",
		States: map[string]orchestrator.State{
			"FETCH": {
				Action: "fetch",
				Transitions: []orchestrator.Transition{
					{Condition: `FETCH["count"] > 0`, Next: "HANDLE_NONEMPTY"},
					{Condition: `FETCH["count"] == 0`, Next: "HANDLE_EMPTY"},
				},
			},
			"HANDLE_NONEMPTY": {Action: "handle_nonempty", Terminal: true},
			"HANDLE_EMPTY":    {Action: "handle_empty", Terminal: true},
		},
	}

	err := it.Execute(context.Background(), bp, "FETCH", interpreter.RunOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestInterpreter_DeadEnd_NoWait(t *testing.T) {
	it, reg := newTestInterpreter(t)
	reg.Register("action", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, nil
	})

	bp := &orchestrator.Intent{
		ID:           "dead-end",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {
				Action: "action",
				// No Next, no transitions, not terminal.
			},
		},
	}

	err := it.Execute(context.Background(), bp, "START", interpreter.RunOptions{})
	if err == nil {
		t.Fatal("expected dead-end error")
	}
}

func TestInterpreter_WaitMechanism(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	it := interpreter.New(reg, zerolog.Nop()).
		WithWait(func(ctx context.Context, stateName string, mem map[string]any) (any, error) {
			// Simulate user confirming.
			return map[string]any{"confirmed": true}, nil
		})

	reg.Register("prompt", func(_ context.Context, _ map[string]any) (any, error) {
		return map[string]any{"suggestions": []string{"link1"}}, nil
	})
	reg.Register("apply", func(_ context.Context, _ map[string]any) (any, error) {
		return "applied", nil
	})

	bp := &orchestrator.Intent{
		ID:           "wait-test",
		InitialState: "PROMPT_USER",
		States: map[string]orchestrator.State{
			"PROMPT_USER": {
				Action: "prompt",
				Transitions: []orchestrator.Transition{
					{Condition: `PROMPT_USER["confirmed"] == true`, Next: "APPLY"},
				},
				// No Next: will trigger Wait.
			},
			"APPLY": {Action: "apply", Terminal: true},
		},
	}

	err := it.Execute(context.Background(), bp, "PROMPT_USER", interpreter.RunOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestInterpreter_TemplateInjection(t *testing.T) {
	it, reg := newTestInterpreter(t)

	var receivedPath string
	reg.Register("fetch_file", func(_ context.Context, _ map[string]any) (any, error) {
		return map[string]any{"path": "/notes/test.md", "content": "hello"}, nil
	})
	reg.Register("process_file", func(_ context.Context, args map[string]any) (any, error) {
		receivedPath, _ = args["file"].(string)
		return "ok", nil
	})

	bp := &orchestrator.Intent{
		ID:           "template-test",
		InitialState: "FETCH",
		States: map[string]orchestrator.State{
			"FETCH": {
				Action: "fetch_file",
				Next:   "PROCESS",
			},
			"PROCESS": {
				Action:   "process_file",
				Args:     map[string]any{"file": `{{index .FETCH "path"}}`},
				Terminal: true,
			},
		},
	}

	err := it.Execute(context.Background(), bp, "FETCH", interpreter.RunOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if receivedPath != "/notes/test.md" {
		t.Errorf("template injection: got %q want %q", receivedPath, "/notes/test.md")
	}
}

func TestInterpreter_ToolError(t *testing.T) {
	it, reg := newTestInterpreter(t)
	reg.Register("failing.tool", func(_ context.Context, _ map[string]any) (any, error) {
		return nil, errors.New("tool failed")
	})

	bp := &orchestrator.Intent{
		ID:           "tool-error",
		InitialState: "RUN",
		States: map[string]orchestrator.State{
			"RUN": {Action: "failing.tool", Terminal: true},
		},
	}

	err := it.Execute(context.Background(), bp, "RUN", interpreter.RunOptions{})
	if err == nil {
		t.Fatal("expected error from tool failure")
	}
}

func TestInterpreter_MissingTool(t *testing.T) {
	it, _ := newTestInterpreter(t)

	bp := &orchestrator.Intent{
		ID:           "missing-tool",
		InitialState: "RUN",
		States: map[string]orchestrator.State{
			"RUN": {Action: "nonexistent.tool", Terminal: true},
		},
	}

	err := it.Execute(context.Background(), bp, "RUN", interpreter.RunOptions{})
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
}

func TestInterpreter_MissingState(t *testing.T) {
	it, _ := newTestInterpreter(t)

	bp := &orchestrator.Intent{
		ID:           "missing-state",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}

	err := it.Execute(context.Background(), bp, "NONEXISTENT", interpreter.RunOptions{})
	if err == nil {
		t.Fatal("expected error for missing start state")
	}
}

func TestInterpreter_ContextCancellation(t *testing.T) {
	it, reg := newTestInterpreter(t)

	// Tool that blocks until context is cancelled.
	reg.Register("blocking.tool", func(ctx context.Context, _ map[string]any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	bp := &orchestrator.Intent{
		ID:           "cancel-test",
		InitialState: "BLOCK",
		States: map[string]orchestrator.State{
			"BLOCK": {Action: "blocking.tool", Terminal: true},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := it.Execute(ctx, bp, "BLOCK", interpreter.RunOptions{})
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
}

func TestInterpreter_OnChange_Callback(t *testing.T) {
	reg := registry.New(zerolog.Nop())

	var changes []string
	it := interpreter.New(reg, zerolog.Nop()).
		WithOnChange(func(_ context.Context, runID, state string, _ map[string]any) {
			changes = append(changes, runID+":"+state)
		})

	reg.Register("a", func(_ context.Context, _ map[string]any) (any, error) { return nil, nil })
	reg.Register("b", func(_ context.Context, _ map[string]any) (any, error) { return nil, nil })

	bp := &orchestrator.Intent{
		ID:           "callback-test",
		InitialState: "A",
		States: map[string]orchestrator.State{
			"A": {Action: "a", Next: "B"},
			"B": {Action: "b", Terminal: true},
		},
	}

	opts := interpreter.RunOptions{RunID: "run-42"}
	err := it.Execute(context.Background(), bp, "A", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Errorf("expected 2 onChange calls, got %d: %v", len(changes), changes)
	}
	if changes[0] != "run-42:A" || changes[1] != "run-42:B" {
		t.Errorf("unexpected changes: %v", changes)
	}
}

func TestInterpreter_InitialMem(t *testing.T) {
	it, reg := newTestInterpreter(t)

	var seenValue any
	reg.Register(
		"check",
		func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	)

	bp := &orchestrator.Intent{
		ID:           "initial-mem-test",
		InitialState: "CHECK",
		States: map[string]orchestrator.State{
			"CHECK": {
				Action: "check",
				Transitions: []orchestrator.Transition{
					{Condition: `seed["value"] == "hello"`, Next: "DONE"},
				},
			},
			"DONE": {Terminal: true},
		},
	}

	opts := interpreter.RunOptions{
		InitialMem: map[string]any{
			"seed": map[string]any{"value": "hello"},
		},
	}
	// seenValue isn't used beyond validation that Execute succeeds.
	_ = seenValue
	err := it.Execute(context.Background(), bp, "CHECK", opts)
	if err != nil {
		t.Fatalf("Execute with initial mem: %v", err)
	}
}
