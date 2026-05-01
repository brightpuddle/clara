package orchestrator

import (
	"testing"

	"go.starlark.net/starlark"
)

func TestYAMLDecode_Scalar(t *testing.T) {
	v, err := callYAMLDecode(t, `"hello"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := v.(starlark.String)
	if !ok {
		t.Fatalf("expected String, got %T", v)
	}
	if s.GoString() != "hello" {
		t.Errorf("expected %q, got %q", "hello", s.GoString())
	}
}

func TestYAMLDecode_Map(t *testing.T) {
	src := "title: My Note\ntags:\n  - go\n  - starlark\n"
	v, err := callYAMLDecode(t, src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("expected Dict, got %T", v)
	}

	titleVal, found, err := d.Get(starlark.String("title"))
	if err != nil || !found {
		t.Fatalf("key 'title' not found: err=%v", err)
	}
	if titleVal.(starlark.String).GoString() != "My Note" {
		t.Errorf("title: got %v", titleVal)
	}

	tagsVal, found, err := d.Get(starlark.String("tags"))
	if err != nil || !found {
		t.Fatalf("key 'tags' not found")
	}
	tagsList, ok := tagsVal.(*starlark.List)
	if !ok {
		t.Fatalf("tags: expected List, got %T", tagsVal)
	}
	if tagsList.Len() != 2 {
		t.Errorf("tags: expected 2 elements, got %d", tagsList.Len())
	}
}

func TestYAMLDecode_List(t *testing.T) {
	src := "- a\n- b\n- c\n"
	v, err := callYAMLDecode(t, src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, ok := v.(*starlark.List)
	if !ok {
		t.Fatalf("expected List, got %T", v)
	}
	if list.Len() != 3 {
		t.Errorf("expected 3 elements, got %d", list.Len())
	}
}

func TestYAMLDecode_Null(t *testing.T) {
	v, err := callYAMLDecode(t, "null")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != starlark.None {
		t.Errorf("expected None, got %v", v)
	}
}

func TestYAMLDecode_InvalidYAML(t *testing.T) {
	_, err := callYAMLDecode(t, ":\t bad yaml {[}")
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestYAMLEncode_Map(t *testing.T) {
	d := starlark.NewDict(2)
	_ = d.SetKey(starlark.String("title"), starlark.String("My Note"))
	_ = d.SetKey(starlark.String("count"), starlark.MakeInt(42))

	v, err := callYAMLEncode(t, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := v.(starlark.String)
	if !ok {
		t.Fatalf("expected String, got %T", v)
	}
	out := s.GoString()
	if out == "" {
		t.Error("expected non-empty YAML output")
	}
	// Round-trip: decode what we encoded
	decoded, err := callYAMLDecode(t, out)
	if err != nil {
		t.Fatalf("round-trip decode failed: %v", err)
	}
	dm, ok := decoded.(*starlark.Dict)
	if !ok {
		t.Fatalf("round-trip: expected Dict, got %T", decoded)
	}
	titleVal, found, _ := dm.Get(starlark.String("title"))
	if !found || titleVal.(starlark.String).GoString() != "My Note" {
		t.Errorf("round-trip: title mismatch, got %v", titleVal)
	}
}

func TestYAMLEncode_List(t *testing.T) {
	list := starlark.NewList([]starlark.Value{
		starlark.String("x"),
		starlark.String("y"),
	})
	v, err := callYAMLEncode(t, list)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := v.(starlark.String).GoString()
	// Round-trip
	decoded, err := callYAMLDecode(t, out)
	if err != nil {
		t.Fatalf("round-trip decode failed: %v", err)
	}
	dl, ok := decoded.(*starlark.List)
	if !ok {
		t.Fatalf("round-trip: expected List, got %T", decoded)
	}
	if dl.Len() != 2 {
		t.Errorf("round-trip: expected 2 elements, got %d", dl.Len())
	}
}

func TestYAMLDecode_FrontmatterPattern(t *testing.T) {
	// Simulate the common Starlark intent pattern:
	// raw.split("---\n", 2) → yaml.decode(parts[1])
	frontmatter := "title: My Note\ntags:\n  - go\n  - clara\ndate: 2024-01-15\n"
	v, err := callYAMLDecode(t, frontmatter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("expected Dict, got %T", v)
	}

	cases := []struct {
		key  string
		want string
	}{
		{"title", "My Note"},
		// yaml.v3 parses bare dates as time.Time; GoToStarlark formats them as RFC3339.
		{"date", "2024-01-15T00:00:00Z"},
	}
	for _, tc := range cases {
		val, found, err := d.Get(starlark.String(tc.key))
		if err != nil || !found {
			t.Errorf("key %q not found", tc.key)
			continue
		}
		if val.(starlark.String).GoString() != tc.want {
			t.Errorf("key %q: expected %q, got %v", tc.key, tc.want, val)
		}
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func callYAMLDecode(t *testing.T, src string) (starlark.Value, error) {
	t.Helper()
	thread := &starlark.Thread{Name: "test"}
	b := starlark.NewBuiltin("yaml.decode", yamlDecode)
	return starlark.Call(thread, b, starlark.Tuple{starlark.String(src)}, nil)
}

func callYAMLEncode(t *testing.T, v starlark.Value) (starlark.Value, error) {
	t.Helper()
	thread := &starlark.Thread{Name: "test"}
	b := starlark.NewBuiltin("yaml.encode", yamlEncode)
	return starlark.Call(thread, b, starlark.Tuple{v}, nil)
}
