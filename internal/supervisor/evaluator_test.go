package supervisor

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/pkg/sdk"
)

type mockLLM struct {
	lastHistory   []string
	result        AnalysisResult
	err           error
	refineResults []AnalysisResult
	refineErrs    []error
	refineCalls   int
}

func (m *mockLLM) AnalyzeEvent(
	ctx context.Context,
	ev CloudEvent,
	history []string,
) (AnalysisResult, error) {
	m.lastHistory = history
	return m.result, m.err
}

func (m *mockLLM) RefineCode(
	ctx context.Context,
	compilerError string,
	failedCode map[string]string,
) (AnalysisResult, error) {
	idx := m.refineCalls
	m.refineCalls++
	if idx < len(m.refineResults) {
		var err error
		if idx < len(m.refineErrs) {
			err = m.refineErrs[idx]
		}
		return m.refineResults[idx], err
	}
	return m.result, m.err
}

func TestEvaluator_ManifestInjection(t *testing.T) {
	log := zerolog.New(io.Discard)
	bus := NewEventBus()

	binDir := t.TempDir()

	// 1. Create a dummy file that represents an actuator binary
	dummyActuatorID := "dummy-actuator"
	dummyFile := filepath.Join(binDir, dummyActuatorID)
	if err := os.WriteFile(dummyFile, []byte("dummy binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	llm := &mockLLM{
		result: AnalysisResult{
			Action:       "ignore",
			HeuristicTTL: 5 * time.Minute,
		},
	}

	eval := NewEvaluator(log, nil, bus, llm, nil, binDir)

	// Since the dummy actuator is just text, calling e.inspectActuator on it will fail.
	// That's perfect because e.OnEvent should fall back gracefully and add it with a description saying it failed to load.
	ev := CloudEvent{
		ID:     "123",
		Source: "test",
		Type:   "test.event",
		Time:   time.Now(),
		Data:   map[string]any{},
	}

	ctx := context.Background()
	if err := eval.OnEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}

	// Assert that "dummy-actuator" was found in history list despite process launch failing
	found := false
	for _, h := range llm.lastHistory {
		if strings.Contains(h, "dummy-actuator") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected dummy-actuator in LLM history, got: %v", llm.lastHistory)
	}

	// Let's manually pre-populate the manifestCache to simulate a successful inspection cache hit.
	// This tests the code path where we query cached manifests without spawning a process.
	eval.RegisterHeuristic("dummy.event", dummyActuatorID, 1*time.Minute)

	// Populate cache manually:
	mCacheField := sdk.ActuatorManifest{
		ID:          dummyActuatorID,
		Description: "A simulated mock actuator for tests",
		Capabilities: []sdk.Capability{
			{Resource: "shell:exec", Description: "Executes commands"},
		},
	}
	eval.mu.Lock()
	eval.manifestCache[dummyActuatorID] = mCacheField
	eval.mu.Unlock()

	// Reset mock LLM history
	llm.lastHistory = nil

	// Trigger another event to check manifest inclusion
	ev2 := CloudEvent{
		ID:     "456",
		Source: "test",
		Type:   "test.event.2",
		Time:   time.Now(),
		Data:   map[string]any{},
	}

	if err := eval.OnEvent(ctx, ev2); err != nil {
		t.Fatal(err)
	}

	// Assert that the detailed manifest description and capability is present in history
	foundDetails := false
	for _, h := range llm.lastHistory {
		if strings.Contains(h, "A simulated mock actuator for tests") &&
			strings.Contains(h, "shell:exec") {
			foundDetails = true
			break
		}
	}
	if !foundDetails {
		t.Fatalf("expected detailed manifest in LLM history, got: %v", llm.lastHistory)
	}
}

func TestEvaluator_SelfHealingLoop(t *testing.T) {
	log := zerolog.New(io.Discard)
	bus := NewEventBus()
	binDir := t.TempDir()
	workspaceDir := t.TempDir()

	// Initialize real builder
	builder, err := NewBuilder(workspaceDir, "")
	if err != nil {
		t.Fatal(err)
	}

	// We'll trigger a "build" action.
	// In the first attempt, the code will have a compiler error (e.g. syntax error).
	// In the refinement attempt, the code will be corrected.
	failedCode := map[string]string{
		"main.go": `package main
import "github.com/brightpuddle/clara/pkg/sdk"
this_is_a_syntax_error
`,
	}

	refinedCode := map[string]string{
		"main.go": `package main

import (
	"context"
	"github.com/brightpuddle/clara/pkg/sdk"
)

type MockActuator struct{}

func (m *MockActuator) Manifest() sdk.ActuatorManifest {
	return sdk.ActuatorManifest{
		ID:          "healing-actuator",
		Description: "A healed actuator",
	}
}

func (m *MockActuator) Execute(ctx context.Context, event sdk.Event) (sdk.Result, error) {
	return sdk.Result{Success: true, Output: "healed"}, nil
}

func main() {
	sdk.Serve(&MockActuator{})
}
`,
	}

	llm := &mockLLM{
		result: AnalysisResult{
			Action:       "build",
			ActuatorID:   "healing-actuator",
			ProposedCode: failedCode,
		},
		refineResults: []AnalysisResult{
			{
				Action:       "build",
				ActuatorID:   "healing-actuator",
				ProposedCode: refinedCode,
			},
		},
	}

	eval := NewEvaluator(log, nil, bus, llm, builder, binDir)

	ev := CloudEvent{
		ID:     "heal-123",
		Source: "test",
		Type:   "test.heal",
		Time:   time.Now(),
		Data:   map[string]any{},
	}

	ctx := context.Background()
	// This will invoke OnEvent. The run will fail because executeActuator expects a working binary plugin
	// but the compilation itself should succeed, and OnEvent will attempt to execute it.
	_ = eval.OnEvent(ctx, ev)

	// Check if refinement was called
	if llm.refineCalls != 1 {
		t.Errorf("expected RefineCode to be called exactly once, got %d", llm.refineCalls)
	}

	// Check if the refined binary got successfully compiled and placed in binDir
	destPath := filepath.Join(binDir, "healing-actuator")
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Errorf("expected compiled binary at %s, but it was not found", destPath)
	}
}

func TestEvaluator_CrashRollback(t *testing.T) {
	log := zerolog.New(io.Discard)
	bus := NewEventBus()
	binDir := t.TempDir()
	workspaceDir := t.TempDir()

	// Initialize real builder
	builder, err := NewBuilder(workspaceDir, "")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Compile a stable actuator first to establish a stable version tag.
	stableCode := map[string]string{
		"main.go": `package main

import (
	"context"
	"github.com/brightpuddle/clara/pkg/sdk"
)

type MockActuator struct{}

func (m *MockActuator) Manifest() sdk.ActuatorManifest {
	return sdk.ActuatorManifest{
		ID:          "rollback-actuator",
		Description: "Stable version",
	}
}

func (m *MockActuator) Execute(ctx context.Context, event sdk.Event) (sdk.Result, error) {
	return sdk.Result{Success: true, Output: "stable"}, nil
}

func main() {
	sdk.Serve(&MockActuator{})
}
`,
	}

	ctx := context.Background()
	res, err := builder.CompileAndVerify(ctx, "rollback-actuator", stableCode)
	if err != nil {
		t.Fatalf("failed to compile stable version: %v", err)
	}
	if !res.Success {
		t.Fatalf("compilation not successful: %s", res.CompilerError)
	}

	// Verify stable tag was created in builder workspace
	out, err := builder.runGit(ctx, "tag", "--list", "stable/rollback-actuator-*")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("expected stable tag to be created")
	}

	// Read content of the stable binary
	stableBinBytes, err := os.ReadFile(res.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}

	// Copy stable binary to evaluator binDir
	evalBinPath := filepath.Join(binDir, "rollback-actuator")
	if err := os.WriteFile(evalBinPath, stableBinBytes, 0o755); err != nil {
		t.Fatal(err)
	}

	eval := NewEvaluator(log, nil, bus, nil, builder, binDir)

	// 2. Corrupt/break the binary in binDir so that it fails to start
	badBytes := []byte("invalid binary content")
	if err := os.WriteFile(evalBinPath, badBytes, 0o755); err != nil {
		t.Fatal(err)
	}

	// 3. Try to execute the actuator. This should fail to start and trigger rollback to the stable version.
	ev := CloudEvent{
		ID:     "test-rollback-123",
		Source: "test",
		Type:   "clara.actuator.run",
		Time:   time.Now(),
		Data:   map[string]any{"actuator_id": "rollback-actuator"},
	}

	err = eval.OnEvent(ctx, ev)
	if err == nil {
		t.Fatal("expected execution to fail on bad binary")
	}

	// 4. Verify that the binary has been restored to the stable version!
	restoredBytes, err := os.ReadFile(evalBinPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(restoredBytes) == string(badBytes) {
		t.Fatal("binary was not rolled back to stable version")
	}

	if len(restoredBytes) != len(stableBinBytes) {
		t.Fatalf(
			"restored binary size mismatch: expected %d bytes, got %d",
			len(stableBinBytes),
			len(restoredBytes),
		)
	}
}
