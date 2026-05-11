package notify

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
)

func TestDummyAsk(t *testing.T) {
	log := zerolog.Nop()
	fn := dummyAsk(log)
	
	args := map[string]any{
		"question": "Does this work?",
	}
	
	res, err := fn(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	resBytes, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	
	expected := `{"selection":"acknowledged"}`
	if string(resBytes) != expected {
		t.Fatalf("expected %s, got %s", expected, string(resBytes))
	}
}