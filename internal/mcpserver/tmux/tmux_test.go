package tmux

import (
	"fmt"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewServer(t *testing.T) {
	svc := New(zerolog.Nop())
	srv := svc.NewServer()

	if srv == nil {
		t.Fatal("NewServer() returned nil")
	}
}

func TestEnsureAvailable(t *testing.T) {
	svc := New(zerolog.Nop())
	_ = svc.ensureAvailable()
}

func TestHelperFunctions(t *testing.T) {
	// test toolErrorResult
	res := toolErrorResult("test", fmt.Errorf("error"))
	if res == nil {
		t.Fatal("toolErrorResult returned nil")
	}
}
