package interpreter_test

import (
	"context"
	"testing"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/rs/zerolog"
)

func TestStarlarkInterpreter_ParameterDefaults(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	
	var printed []string
	it := interpreter.NewStarlark(reg, zerolog.Nop()).
		WithOnStep(func(ctx context.Context, event interpreter.StepEvent) {
			if event.Action == "print" {
				printed = append(printed, event.Result.(string))
			}
		})

	script := `
def main(name = "World"):
    print("NAME: " + name)
`
	intent := &orchestrator.Intent{
		ID:           "test",
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script:       script,
	}

	// Case 1: HandlerArgs is nil. Expected: name = "World"
	err := it.Execute(context.Background(), intent, "", interpreter.RunOptions{
		RunID:       "run-1",
		HandlerArgs: nil,
	})
	if err != nil {
		t.Errorf("Execute with nil args failed: %v", err)
	}
	if len(printed) == 0 || printed[0] != "NAME: World" {
		t.Errorf("Expected 'NAME: World', got %v", printed)
	}
	printed = nil

	// Case 2: HandlerArgs is empty map.
	// Expected: name = "World" (default value)
	err = it.Execute(context.Background(), intent, "", interpreter.RunOptions{
		RunID:       "run-2",
		HandlerArgs: map[string]any{},
	})
	if err != nil {
		t.Errorf("Execute with empty map failed: %v", err)
	}
	if len(printed) == 0 || printed[0] != "NAME: World" {
		t.Errorf("Expected 'NAME: World' for empty map, got %v", printed)
	}
	printed = nil

	// Case 3: HandlerArgs has "name".
	err = it.Execute(context.Background(), intent, "", interpreter.RunOptions{
		RunID:       "run-3",
		HandlerArgs: map[string]any{"name": "Clara"},
	})
	if err != nil {
		t.Errorf("Execute with name=Clara failed: %v", err)
	}
	if len(printed) == 0 || printed[0] != "NAME: Clara" {
		t.Errorf("Expected 'NAME: Clara', got %v", printed)
	}
	printed = nil

	// Case 4: HandlerArgs is a typed nil map.
	// Expected: name = "World" (default value)
	var nilMap map[string]any
	err = it.Execute(context.Background(), intent, "", interpreter.RunOptions{
		RunID:       "run-4",
		HandlerArgs: nilMap,
	})
	if err != nil {
		t.Errorf("Execute with typed nil map failed: %v", err)
	}
	if len(printed) == 0 || printed[0] != "NAME: World" {
		t.Errorf("Expected 'NAME: World' for typed nil map, got %v", printed)
	}
}
