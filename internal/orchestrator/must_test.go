package orchestrator

import (
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

func TestMust_Errors(t *testing.T) {
	predeclared := starlark.StringDict{
		"must": MustModule,
	}
	
	tests := []struct {
		name     string
		script   string
		contains []string
	}{
		{
			name:   "true fail",
			script: "must.true(False)",
			contains: []string{"test.star:1:10", "must.true failed: expected True, got False"},
		},
		{
			name:   "true fail with msg",
			script: "must.true(False, msg='custom message')",
			contains: []string{"test.star:1:10", "must.true failed: custom message"},
		},
		{
			name:   "eq fail",
			script: "must.eq(1, 2)",
			contains: []string{"test.star:1:8", "must.eq failed: 1 != 2"},
		},
		{
			name:   "eq fail with msg",
			script: "must.eq(1, 2, msg='not equal')",
			contains: []string{"test.star:1:8", "must.eq failed: not equal"},
		},
		{
			name:   "neq fail",
			script: "must.neq(1, 1)",
			contains: []string{"test.star:1:9", "must.neq failed: 1 == 1"},
		},
		{
			name:   "false fail",
			script: "must.false(True)",
			contains: []string{"test.star:1:11", "must.false failed: expected False, got True"},
		},
		{
			name:   "fails fail",
			script: "must.fails(lambda: 1)",
			contains: []string{"test.star:1:11", "must.fails failed: expected function to fail but it succeeded"},
		},
		{
			name: "nested call",
			script: `def test():
    must.true(False)
test()`,
			contains: []string{"test.star:2:14", "must.true failed: expected True, got False"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread := &starlark.Thread{Name: "test"}
			_, err := starlark.ExecFile(thread, "test.star", tt.script, predeclared)
			if err == nil {
				t.Fatal("expected error")
			}
			got := err.Error()
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("error %q does not contain %q", got, want)
				}
			}
			t.Logf("Got error: %v", err)
		})
	}
}
