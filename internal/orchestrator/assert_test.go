package orchestrator_test

import (
	"testing"
	"go.starlark.net/starlark"
	"github.com/brightpuddle/clara/internal/orchestrator"
)

func TestAssertModule(t *testing.T) {
	thread := &starlark.Thread{Name: "test"}
	env := starlark.StringDict{"assert": orchestrator.AssertModule}
	
	validScripts := []string{
		`assert.eq(1, 1)`,
		`assert.neq(1, 2)`,
		`assert.true(1 == 1)`,
		`assert.false(1 == 2)`,
	}
	for _, script := range validScripts {
		if _, err := starlark.ExecFile(thread, "test.star", script, env); err != nil {
			t.Errorf("script failed unexpectedly: %q, %v", script, err)
		}
	}
	
	invalidScripts := []string{
		`assert.eq(1, 2)`,
		`assert.neq(1, 1)`,
		`assert.true(False)`,
		`assert.false(True)`,
	}
	for _, script := range invalidScripts {
		if _, err := starlark.ExecFile(thread, "test.star", script, env); err == nil {
			t.Errorf("script succeeded unexpectedly: %q", script)
		}
	}
}
