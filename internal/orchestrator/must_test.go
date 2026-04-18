package orchestrator_test

import (
	"testing"
	"go.starlark.net/starlark"
	"github.com/brightpuddle/clara/internal/orchestrator"
)

func TestMustModule(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	env := starlark.StringDict{"must": orchestrator.MustModule}
	
	validScripts := []string{
		`must.eq(1, 1)`,
		`must.neq(1, 2)`,
		`must.true(1 == 1)`,
		`must.false(1 == 2)`,
		`must.fails(lambda: fail("expected"))`,
	}
	for _, script := range validScripts {
		if _, err := starlark.ExecFile(thread, "test.star", script, env); err != nil {
			t.Errorf("script failed unexpectedly: %q, %v", script, err)
		}
	}
	
	invalidScripts := []string{
		`must.eq(1, 2)`,
		`must.neq(1, 1)`,
		`must.true(False)`,
		`must.false(True)`,
		`must.fails(lambda: 1 + 1)`,
	}
	for _, script := range invalidScripts {
		if _, err := starlark.ExecFile(thread, "test.star", script, env); err == nil {
			t.Errorf("script succeeded unexpectedly: %q", script)
		}
	}
}
