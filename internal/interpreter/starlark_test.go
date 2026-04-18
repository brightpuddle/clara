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

func TestStarlarkInterpreter_ResolveEventMethods(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	// Register 'theme' as a known namespace (via a server)
	reg.AddServer(registry.NewHTTPMCPServer("theme", "Theme server", "http://localhost", "", false, zerolog.Nop()))

	it := interpreter.NewStarlark(reg, zerolog.Nop())

	// This should NOT fail during execution/attribute lookup, even though on_change is not a tool.
	// It's used as a reference for clara.on().
	intent := &orchestrator.Intent{
		ID:           "event-resolve",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script: `
def main():
    # theme.on_change should be a callable builtin
    if type(theme.on_change) != "builtin_function_or_method":
        fail("theme.on_change should be a builtin, got %s" % type(theme.on_change))
    # Calling it should fail with "disconnected or has no tool"
    # because it's not registered as a tool.
    return "ok"
`,
	}

	if err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "run-event"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestStarlarkInterpreter_ReplaysToolCalls(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	calls := 0
	reg.Register("test.echo", func(_ context.Context, args map[string]any) (any, error) {
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
def main():
    return test.echo(value = "hello")
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
def main():
    return clara.wait("approval", prompt = "Continue?")
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
def main():
    return clara.wait("approval", prompt = "Ship it?")
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

func TestStarlarkInterpreter_Search(t *testing.T) {
	reg := registry.New(zerolog.Nop())

	reg.Register("zk.note_search", func(_ context.Context, args map[string]any) (any, error) {
		limit := 0
		if l, ok := args["limit"].(int); ok {
			limit = l
		}
		res := []map[string]any{{"name": "note1"}, {"name": "note2"}}
		if limit > 0 && limit < len(res) {
			res = res[:limit]
		}
		return res, nil
	})
	reg.Register("webex.search_messages", func(_ context.Context, _ map[string]any) (any, error) {
		return []map[string]any{{"text": "msg1"}}, nil
	})
	reg.Register("mail.search", func(_ context.Context, _ map[string]any) (any, error) {
		return []map[string]any{{"subject": "mail1"}}, nil
	})

	it := interpreter.NewStarlark(reg, zerolog.Nop()).
		WithOnChange(func(_ context.Context, _ string, _ string, _ string, mem map[string]any) {
			res, ok := mem["main_result"].(map[string]any)
			if !ok {
				return
			}
			if q, ok := mem["query"].(string); ok && q == "clara" {
				if len(res["zk"].([]any)) != 2 || len(res["webex"].([]any)) != 1 || len(res["email"].([]any)) != 1 {
					t.Errorf("unexpected search result: %v", res)
				}
			}
			if l, ok := mem["limit"].(int64); ok && l == 1 {
				if len(res["zk"].([]any)) != 1 {
					t.Errorf("expected 1 result for zk with limit=1, got %d", len(res["zk"].([]any)))
				}
			}
		})

	// Test 1: basic search
	intent := &orchestrator.Intent{
		ID:           "search-script",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script: `
def main():
    query = "clara"
    return clara.search(query = query)
`,
	}

	if err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "run-search"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Test 2: search with limit
	intentLimit := &orchestrator.Intent{
		ID:           "search-limit-script",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script: `
def main():
    limit = 1
    return clara.search(query = "clara", limit = limit)
`,
	}

	if err := it.Execute(context.Background(), intentLimit, "", interpreter.RunOptions{RunID: "run-search-limit"}); err != nil {
		t.Fatalf("Execute with limit: %v", err)
	}
}

func TestStarlarkInterpreter_Assert(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	it := interpreter.NewStarlark(reg, zerolog.Nop())

	intent := &orchestrator.Intent{
		ID:           "assert-script",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script: `
def main():
    assert.eq(1, 1)
    assert.true(True)
    assert.false(False)
    assert.neq(1, 2)
    assert.fails(lambda: assert.eq(1, 2))
    return "ok"
`,
	}

	if err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{RunID: "run-assert"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}
