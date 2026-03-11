package registry_test

import (
	"context"
	"testing"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/rs/zerolog"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := registry.New(zerolog.Nop())

	var called bool
	reg.Register("test.tool", func(ctx context.Context, args map[string]any) (any, error) {
		called = true
		return "result", nil
	})

	tool, ok := reg.Get("test.tool")
	if !ok {
		t.Fatal("expected tool to be registered")
	}
	result, err := tool(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("tool was not called")
	}
	if result != "result" {
		t.Errorf("got %v, want %q", result, "result")
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("expected false for missing tool")
	}
}

func TestRegistry_Call(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	reg.Register("echo", func(ctx context.Context, args map[string]any) (any, error) {
		return args["msg"], nil
	})

	result, err := reg.Call(context.Background(), "echo", map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if result != "hello" {
		t.Errorf("got %v, want %q", result, "hello")
	}
}

func TestRegistry_CallMissing(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	_, err := reg.Call(context.Background(), "missing.tool", nil)
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
}

func TestRegistry_Has(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	if reg.Has("nope") {
		t.Error("expected false")
	}
	reg.Register("yes.tool", func(_ context.Context, _ map[string]any) (any, error) { return nil, nil })
	if !reg.Has("yes.tool") {
		t.Error("expected true")
	}
}

func TestRegistry_Names(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	reg.Register("a.foo", func(_ context.Context, _ map[string]any) (any, error) { return nil, nil })
	reg.Register("b.bar", func(_ context.Context, _ map[string]any) (any, error) { return nil, nil })
	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestRegistry_Register_Overwrites(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	reg.Register("tool", func(_ context.Context, _ map[string]any) (any, error) { return "v1", nil })
	reg.Register("tool", func(_ context.Context, _ map[string]any) (any, error) { return "v2", nil })
	result, _ := reg.Call(context.Background(), "tool", nil)
	if result != "v2" {
		t.Errorf("expected v2, got %v", result)
	}
}
