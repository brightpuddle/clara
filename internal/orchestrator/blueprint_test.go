package orchestrator_test

import (
	"encoding/json"
	"testing"

	"github.com/brightpuddle/clara/internal/orchestrator"
)

func TestBlueprintValidate_Valid(t *testing.T) {
	b := &orchestrator.Blueprint{
		ID:           "test-blueprint",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {
				Next: "END",
			},
			"END": {
				Terminal: true,
			},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("expected valid blueprint, got error: %v", err)
	}
}

func TestBlueprintValidate_MissingID(t *testing.T) {
	b := &orchestrator.Blueprint{
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestBlueprintValidate_MissingInitialState(t *testing.T) {
	b := &orchestrator.Blueprint{
		ID: "test",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for missing initial_state")
	}
}

func TestBlueprintValidate_InitialStateNotFound(t *testing.T) {
	b := &orchestrator.Blueprint{
		ID:           "test",
		InitialState: "MISSING",
		States: map[string]orchestrator.State{
			"START": {Terminal: true},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for initial_state referencing nonexistent state")
	}
}

func TestBlueprintValidate_TransitionToMissingState(t *testing.T) {
	b := &orchestrator.Blueprint{
		ID:           "test",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {
				Transitions: []orchestrator.Transition{
					{Condition: "true", Next: "DOES_NOT_EXIST"},
				},
			},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for transition referencing nonexistent state")
	}
}

func TestBlueprintValidate_NextToMissingState(t *testing.T) {
	b := &orchestrator.Blueprint{
		ID:           "test",
		InitialState: "START",
		States: map[string]orchestrator.State{
			"START": {
				Next: "DOES_NOT_EXIST",
			},
		},
	}
	err := b.Validate()
	if err == nil {
		t.Fatal("expected error for next referencing nonexistent state")
	}
}

func TestParseBlueprint_RoundTrip(t *testing.T) {
	input := &orchestrator.Blueprint{
		ID:           "roundtrip",
		Description:  "test",
		InitialState: "A",
		States: map[string]orchestrator.State{
			"A": {
				Action: "some.tool",
				Args:   map[string]any{"key": "val"},
				Next:   "B",
			},
			"B": {Terminal: true},
		},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := orchestrator.ParseBlueprint(data)
	if err != nil {
		t.Fatalf("ParseBlueprint failed: %v", err)
	}
	if got.ID != input.ID {
		t.Errorf("ID mismatch: got %q want %q", got.ID, input.ID)
	}
	if got.InitialState != input.InitialState {
		t.Errorf("InitialState mismatch: got %q want %q", got.InitialState, input.InitialState)
	}
}

func TestParseBlueprint_InvalidJSON(t *testing.T) {
	_, err := orchestrator.ParseBlueprint([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
