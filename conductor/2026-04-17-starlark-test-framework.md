# Unified Assertion Framework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide a Starlark testing harness (`clara test`) and a unified `must` module for both tests and runtime assertions.

**Architecture:** Create a `must` module providing `eq`, `neq`, `true`, `false`, and `fails` methods. Make this module available globally in Starlark. Collect functions with a `test_` prefix during compilation and execute them with an isolated in-memory DB in a new `clara test` CLI command. Ensure `*_test.star` files are ignored by the standard supervisor.

**Tech Stack:** Go, `go.starlark.net`

---

### Task 1: Add `Tests` list to the Intent struct

**Files:**
- Modify: `internal/orchestrator/intent.go`

- [ ] **Step 1: Write the minimal implementation**

```go
type Intent struct {
	ID           string            `json:"id"                      yaml:"id"`
	Description  string            `json:"description,omitempty"   yaml:"description,omitempty"`
	Tasks        []Task            `json:"tasks,omitempty"         yaml:"tasks,omitempty"`
	Tests        []string          `json:"tests,omitempty"         yaml:"tests,omitempty"`
	WorkflowType string            `json:"workflow_type,omitempty" yaml:"workflow_type,omitempty"`
	Script       string            `json:"script,omitempty"        yaml:"script,omitempty"`
	InitialState string            `json:"initial_state,omitempty" yaml:"initial_state,omitempty"`
	Context      map[string]string `json:"context,omitempty"       yaml:"context,omitempty"` // alias → mcp:// URI
	States       map[string]State  `json:"states,omitempty"        yaml:"states,omitempty"`
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/orchestrator/intent.go
git commit -m "feat: add Tests field to Intent struct"
```

### Task 2: Create the `must` module

**Files:**
- Create: `internal/orchestrator/must.go`
- Create: `internal/orchestrator/must_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
	}
	for _, script := range invalidScripts {
		if _, err := starlark.ExecFile(thread, "test.star", script, env); err == nil {
			t.Errorf("script succeeded unexpectedly: %q", script)
		}
	}
}
```

- [ ] **Step 2: Verify test fails**

Run: `go test ./internal/orchestrator -run TestMustModule`
Expected: build error (module not found)

- [ ] **Step 3: Implement the `must` module**

```go
package orchestrator

import (
	"fmt"
	"go.starlark.net/starlark"
)

var MustModule = &mustModule{}

type mustModule struct{}

func (m *mustModule) String() string        { return "<module must>" }
func (m *mustModule) Type() string          { return "module" }
func (m *mustModule) Freeze()               {}
func (m *mustModule) Truth() starlark.Bool  { return true }
func (m *mustModule) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable: %s", m.Type()) }

func (m *mustModule) Attr(name string) (starlark.Value, error) {
	switch name {
	case "eq":
		return starlark.NewBuiltin("eq", mustEq), nil
	case "neq":
		return starlark.NewBuiltin("neq", mustNeq), nil
	case "true":
		return starlark.NewBuiltin("true", mustTrue), nil
	case "false":
		return starlark.NewBuiltin("false", mustFalse), nil
	case "fails":
		return starlark.NewBuiltin("fails", mustFails), nil
	default:
		return nil, nil
	}
}

func (m *mustModule) AttrNames() []string {
	return []string{"eq", "neq", "true", "false", "fails"}
}

func mustEq(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, y starlark.Value
	if err := starlark.UnpackArgs("eq", args, kwargs, "x", &x, "y", &y); err != nil {
		return nil, err
	}
	if ok, err := starlark.Equal(x, y); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("must.eq failed: %v != %v", x, y)
	}
	return starlark.None, nil
}

func mustNeq(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var x, y starlark.Value
	if err := starlark.UnpackArgs("neq", args, kwargs, "x", &x, "y", &y); err != nil {
		return nil, err
	}
	if ok, err := starlark.Equal(x, y); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("must.neq failed: %v == %v", x, y)
	}
	return starlark.None, nil
}

func mustTrue(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond starlark.Value
	if err := starlark.UnpackArgs("true", args, kwargs, "cond", &cond); err != nil {
		return nil, err
	}
	if !cond.Truth() {
		return nil, fmt.Errorf("must.true failed: expected True, got False")
	}
	return starlark.None, nil
}

func mustFalse(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var cond starlark.Value
	if err := starlark.UnpackArgs("false", args, kwargs, "cond", &cond); err != nil {
		return nil, err
	}
	if cond.Truth() {
		return nil, fmt.Errorf("must.false failed: expected False, got True")
	}
	return starlark.None, nil
}

func mustFails(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var f starlark.Callable
	if err := starlark.UnpackArgs("fails", args, kwargs, "f", &f); err != nil {
		return nil, err
	}
	_, err := starlark.Call(thread, f, nil, nil)
	if err == nil {
		return nil, fmt.Errorf("must.fails failed: expected function to fail but it succeeded")
	}
	return starlark.None, nil
}
```

- [ ] **Step 4: Verify test passes**

Run: `go test ./internal/orchestrator -run TestMustModule`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/must.go internal/orchestrator/must_test.go
git commit -m "feat: add starlark must module for runtime assertions and testing"
```

### Task 3: Expose `must` globally and extract tests during load

**Files:**
- Modify: `internal/orchestrator/intent_loader.go`
- Modify: `internal/interpreter/starlark.go`

- [ ] **Step 1: Write the minimal implementation**

In `internal/orchestrator/intent_loader.go`, update `CompileStarlarkIntent`:
```go
	predeclared := starlark.StringDict{
		"clara":  &claraBuiltins{loader: loader},
		"tui":    &dummyNamespaceProxy{name: "tui", namespaces: namespaces},
		"must":   MustModule,
	}
```
Further down in `CompileStarlarkIntent`, after `globals, err := starlark.ExecFile(...)`, add extraction for `test_` functions:
```go
	// Extract tests
	var tests []string
	for name, val := range globals {
		if strings.HasPrefix(name, "test_") {
			if _, ok := val.(starlark.Callable); ok {
				tests = append(tests, name)
			}
		}
	}
	loader.intent.Tests = tests

	// Auto-register main() ... (existing code)
```

In `internal/interpreter/starlark.go`, update `Execute`:
```go
	predeclared := starlark.StringDict{
		"clara":  &claraRuntimeBuiltins{rt: runtime},
		"tui":    &NamespaceProxy{rt: runtime, name: "tui"},
		"must":   orchestrator.MustModule,
	}
```

- [ ] **Step 2: Commit**

```bash
git add internal/orchestrator/intent_loader.go internal/interpreter/starlark.go
git commit -m "feat: expose must module globally and collect tests on load"
```

### Task 4: Exclude `*_test.star` from the Supervisor watcher

**Files:**
- Modify: `internal/supervisor/supervisor.go`

- [ ] **Step 1: Write the minimal implementation**

In `internal/supervisor/supervisor.go`, update `isIntentFile`:
```go
func isIntentFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".star") && !strings.HasSuffix(path, "_test.star")
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/supervisor/supervisor.go
git commit -m "fix: ignore test files in supervisor task loader"
```

### Task 5: Implement `clara test` command

**Files:**
- Create: `cmd/clara/test.go`
- Modify: `cmd/clara/main.go` (if necessary to add the command to rootCmd)

- [ ] **Step 1: Create the command implementation**

Create `cmd/clara/test.go`:
```go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test [paths...]",
	Short: "Run Starlark tests (*_test.star)",
	RunE:  runTests,
}

func init() {
	rootCmd.AddCommand(testCmd)
}

func runTests(cmd *cobra.Command, args []string) error {
	paths := args
	if len(paths) == 0 {
		paths = []string{cfg.TasksDir()}
	}

	var testFiles []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if strings.HasSuffix(p, "_test.star") {
				testFiles = append(testFiles, p)
			}
			continue
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, "_test.star") {
				testFiles = append(testFiles, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	if len(testFiles) == 0 {
		fmt.Println("No tests found.")
		return nil
	}

	logger := buildLogger()
	// Use isolated in-memory db for testing
	db, err := store.Open(":memory:", logger)
	if err != nil {
		return errors.Wrap(err, "open test database")
	}
	defer db.Close()

	reg := registry.New(logger)
	if err := addMCPServers(reg, logger); err != nil {
		return err
	}
	registerPermanentTUITools(reg, db, logger)

	ctx := context.Background()
	if err := reg.StartServers(ctx); err != nil {
		return errors.Wrap(err, "start MCP servers")
	}
	defer reg.StopServers()
	_ = reg.WaitReady(ctx)

	passed := 0
	failed := 0

	for _, file := range testFiles {
		fmt.Printf("=== RUN   %s\n", file)
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("--- FAIL: %s (read error: %v)\n", file, err)
			failed++
			continue
		}

		namespaces := []string{"llm", "search", "clara_tui"}
		if cfg != nil {
			for _, srv := range cfg.MCPServers {
				namespaces = append(namespaces, srv.Name)
			}
		}

		intent, err := orchestrator.LoadIntentFile(file, data, namespaces)
		if err != nil {
			fmt.Printf("--- FAIL: %s (parse error: %v)\n", file, err)
			failed++
			continue
		}

		if len(intent.Tests) == 0 {
			fmt.Printf("--- SKIP: %s (no test_ functions found)\n", file)
			continue
		}

		for _, testName := range intent.Tests {
			fmt.Printf("    --- RUN   %s\n", testName)
			
			it := interpreter.NewStarlark(reg, logger)
			
			start := time.Now()
			err := it.Execute(ctx, intent, "", interpreter.RunOptions{
				Entrypoint: testName,
			})
			dur := time.Since(start)

			if err != nil {
				fmt.Printf("    --- FAIL: %s (%v)\n", testName, dur)
				failed++
			} else {
				fmt.Printf("    --- PASS: %s (%v)\n", testName, dur)
				passed++
			}
		}
	}

	fmt.Printf("\nTests: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return errors.New("tests failed")
	}
	return nil
}
```

- [ ] **Step 2: Ensure `testCmd` compiles**

Run: `go build ./cmd/clara`

- [ ] **Step 3: Commit**

```bash
git add cmd/clara/test.go
git commit -m "feat: add clara test command to execute starlark tests"
```
