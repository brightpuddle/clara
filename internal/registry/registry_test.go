package registry_test

import (
	"context"
	"testing"

	"github.com/brightpuddle/clara/internal/registry"
	"github.com/mark3labs/mcp-go/mcp"
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

func TestRegistry_CallNormalizesJSONObjectString(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	reg.Register("echo", func(context.Context, map[string]any) (any, error) {
		return "{\"key\":\"value\"}", nil
	})

	result, err := reg.Call(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	obj, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T (%#v)", result, result)
	}
	if obj["key"] != "value" {
		t.Fatalf("unexpected normalized object: %#v", obj)
	}
}

func TestRegistry_CallNormalizesJSONArrayString(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	reg.Register("echo", func(context.Context, map[string]any) (any, error) {
		return "[\"one\",\"two\"]", nil
	})

	result, err := reg.Call(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected slice result, got %T (%#v)", result, result)
	}
	if len(arr) != 2 || arr[0] != "one" || arr[1] != "two" {
		t.Fatalf("unexpected normalized array: %#v", arr)
	}
}

func TestRegistry_CallLeavesInvalidJSONStringUnchanged(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	reg.Register("echo", func(context.Context, map[string]any) (any, error) {
		return "{\"key\":", nil
	})

	result, err := reg.Call(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	text, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T (%#v)", result, result)
	}
	if text != "{\"key\":" {
		t.Fatalf("unexpected string result: %q", text)
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
	reg.Register(
		"yes.tool",
		func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	)
	if !reg.Has("yes.tool") {
		t.Error("expected true")
	}
}

func TestRegistry_Names(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	reg.Register(
		"a.foo",
		func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	)
	reg.Register(
		"b.bar",
		func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	)
	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestRegistry_Register_Overwrites(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	reg.Register(
		"tool",
		func(_ context.Context, _ map[string]any) (any, error) { return "v1", nil },
	)
	reg.Register(
		"tool",
		func(_ context.Context, _ map[string]any) (any, error) { return "v2", nil },
	)
	result, _ := reg.Call(context.Background(), "tool", nil)
	if result != "v2" {
		t.Errorf("expected v2, got %v", result)
	}
}

func TestRegistry_RegisterWithSpecStoresMetadata(t *testing.T) {
	reg := registry.New(zerolog.Nop())
	spec := mcp.NewTool("db.query",
		mcp.WithDescription("Execute a SQL query and return the results."),
		mcp.WithString("sql", mcp.Required(), mcp.Description("SQL query text.")),
	)

	reg.RegisterWithSpecAndExamples(spec, []string{"db.query(sql: str)"}, func(
		_ context.Context, _ map[string]any,
	) (any, error) {
		return nil, nil
	})

	info, ok := reg.Tool("db.query")
	if !ok {
		t.Fatal("expected tool info to be available")
	}
	if info.Description != spec.Description {
		t.Fatalf("description: got %q want %q", info.Description, spec.Description)
	}
	if info.Spec.Name != "db.query" {
		t.Fatalf("spec name: got %q want %q", info.Spec.Name, "db.query")
	}
	if len(info.Spec.InputSchema.Required) != 1 || info.Spec.InputSchema.Required[0] != "sql" {
		t.Fatalf("required params: got %v want [sql]", info.Spec.InputSchema.Required)
	}
	if len(info.Examples) != 1 || info.Examples[0] != "db.query(sql: str)" {
		t.Fatalf("examples: got %v", info.Examples)
	}
}

func TestUnregisterNamespace(t *testing.T) {
	reg := registry.New(zerolog.Nop())

	// Register tools in different namespaces
	reg.Register("shell.ls", func(_ context.Context, _ map[string]any) (any, error) { return nil, nil })
	reg.Register("shell.pwd", func(_ context.Context, _ map[string]any) (any, error) { return nil, nil })
	reg.Register("fs.read", func(_ context.Context, _ map[string]any) (any, error) { return nil, nil })

	// Register namespace metadata
	reg.RegisterNamespaceDescription("shell", "Shell commands")

	// Verify they exist
	if !reg.Has("shell.ls") || !reg.Has("shell.pwd") || !reg.Has("fs.read") {
		t.Fatal("expected all tools to be registered")
	}

	// Unregister shell namespace
	reg.UnregisterNamespace("shell")

	// Verify shell tools are gone
	if reg.Has("shell.ls") {
		t.Error("expected shell.ls to be removed")
	}
	if reg.Has("shell.pwd") {
		t.Error("expected shell.pwd to be removed")
	}

	// Verify fs tool remains
	if !reg.Has("fs.read") {
		t.Error("expected fs.read to remain")
	}

	// Verify metadata is cleared
	if reg.NamespaceDescription("shell") != "" {
		t.Error("expected shell namespace description to be cleared")
	}
}
