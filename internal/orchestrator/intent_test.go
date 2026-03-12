package orchestrator_test

import (
	"encoding/json"
	"testing"

	"github.com/brightpuddle/clara/internal/orchestrator"
)

func TestIntentValidate_Valid(t *testing.T) {
	b := &orchestrator.Intent{
		ID:           "test-intent",
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
		t.Fatalf("expected valid intent, got error: %v", err)
	}
}

func TestIntentValidate_MissingID(t *testing.T) {
	b := &orchestrator.Intent{
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

func TestIntentValidate_MissingInitialState(t *testing.T) {
	b := &orchestrator.Intent{
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

func TestIntentValidate_InitialStateNotFound(t *testing.T) {
	b := &orchestrator.Intent{
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

func TestIntentValidate_TransitionToMissingState(t *testing.T) {
	b := &orchestrator.Intent{
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

func TestIntentValidate_NextToMissingState(t *testing.T) {
	b := &orchestrator.Intent{
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

func TestParseIntent_RoundTrip(t *testing.T) {
	input := &orchestrator.Intent{
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
	got, err := orchestrator.ParseIntent(data)
	if err != nil {
		t.Fatalf("ParseIntent failed: %v", err)
	}
	if got.ID != input.ID {
		t.Errorf("ID mismatch: got %q want %q", got.ID, input.ID)
	}
	if got.InitialState != input.InitialState {
		t.Errorf("InitialState mismatch: got %q want %q", got.InitialState, input.InitialState)
	}
}

func TestParseIntent_InvalidJSON(t *testing.T) {
	_, err := orchestrator.ParseIntent([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
