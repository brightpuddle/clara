package main

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/cockroachdb/errors"
	"github.com/hashicorp/go-plugin"
	"github.com/rs/zerolog"
)

func executeIntentRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	entrypoint string,
	args any,
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) error {
	switch intent.WorkflowKind() {
	case orchestrator.WorkflowTypeNative:
		return executeNativeRun(ctx, intent, runID, entrypoint, args, reg, db, log)
	default:
		return executeStateMachineRun(ctx, intent, runID, reg, db, log)
	}
}

func executeNativeRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	entrypoint string,
	args any,
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) error {
	// For native plugins, the 'Script' field contains the path to the binary
	pluginPath := intent.Script
	if pluginPath == "" {
		return errors.New("native intent missing plugin path")
	}

	// 1. Setup the plugin client
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"intent": &contract.IntentPlugin{},
		},
		Cmd:    exec.Command(pluginPath),
		Logger: buildHCLogAdapter(log, intent.ID),
	})
	defer client.Kill()

	rpcClient, err := client.Client()
	if err != nil {
		return errors.Wrap(err, "connect to native intent plugin")
	}

	raw, err := rpcClient.Dispense("intent")
	if err != nil {
		return errors.Wrap(err, "dispense native intent")
	}

	nativeIntent := raw.(contract.Intent)

	// 2. Setup a host-side shell integration for the plugin to use
	// For now, we use a simple wrapper that uses the registry's 'shell.run' tool if available,
	// or falls back to os/exec for the PoC.
	shell := &registryShell{
		ctx: ctx,
		reg: reg,
		log: log,
	}

	// 3. Execute
	name := "World" // Default for the PoC
	if s, ok := args.(string); ok {
		name = s
	} else if m, ok := args.(map[string]any); ok {
		if n, ok := m["name"].(string); ok {
			name = n
		}
	}

	appendRunEvent(ctx, db, log, interpreter.StepEvent{
		RunID:    runID,
		IntentID: intent.ID,
		State:    "NATIVE",
		Action:   "execute",
		Args:     map[string]any{"name": name},
	})

	err = nativeIntent.Execute(name, shell)
	if err != nil {
		appendRunEvent(ctx, db, log, interpreter.StepEvent{
			RunID:    runID,
			IntentID: intent.ID,
			State:    "NATIVE",
			Action:   "execute",
			Error:    err.Error(),
		})
		return err
	}

	appendRunEvent(ctx, db, log, interpreter.StepEvent{
		RunID:    runID,
		IntentID: intent.ID,
		State:    "NATIVE",
		Action:   "execute",
		Result:   "completed",
	})

	return nil
}

type registryShell struct {
	ctx context.Context
	reg *registry.Registry
	log zerolog.Logger
}

func (s *registryShell) Run(command string) (string, error) {
	s.log.Debug().Str("command", command).Msg("native intent requested shell execution")

	// Try calling the 'shell.run' tool via registry
	if s.reg.Has("shell.run") {
		res, err := s.reg.Call(s.ctx, "shell.run", map[string]any{"command": command})
		if err == nil {
			if s, ok := res.(string); ok {
				return s, nil
			}
			return fmt.Sprintf("%v", res), nil
		}
	}

	// Fallback to local execution for PoC
	cmd := exec.Command("bash", "-c", command)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func executeStateMachineRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) error {
	it := interpreter.New(reg, log).WithOnStep(func(ctx context.Context, event interpreter.StepEvent) {
		appendRunEvent(ctx, db, log, event)
	})
	return it.Execute(ctx, intent, intent.InitialState, interpreter.RunOptions{RunID: runID})
}

func runIntentInBackground(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	entrypoint string,
	args any,
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) {
	var mem map[string]any
	if m, ok := args.(map[string]any); ok {
		mem = m
	}

	if err := db.InitRun(
		context.WithoutCancel(ctx),
		runID,
		intent.ID,
		initialRunState(intent),
		intent.WorkflowKind(),
		entrypoint,
		intent.Script,
		mem,
	); err != nil {
		log.Error().Err(err).Str("run_id", runID).Msg("failed to initialize run")
		return
	}

	err := executeIntentRun(ctx, intent, runID, entrypoint, args, reg, db, log)
	if err != nil {
		if finishErr := db.FinishRun(context.WithoutCancel(ctx), runID, "failed", err.Error()); finishErr != nil {
			log.Warn().
				Err(finishErr).
				Str("run_id", runID).
				Msg("failed to persist run failure")
		}
		return
	}

	if finishErr := db.FinishRun(context.WithoutCancel(ctx), runID, "completed", ""); finishErr != nil {
		log.Warn().
			Err(finishErr).
			Str("run_id", runID).
			Msg("failed to persist run completion")
	}
}

func initialRunState(intent *orchestrator.Intent) string {
	if intent.WorkflowKind() == orchestrator.WorkflowTypeNative {
		return "NATIVE"
	}
	return intent.InitialState
}

func appendRunEvent(
	ctx context.Context,
	db *store.Store,
	log zerolog.Logger,
	event interpreter.StepEvent,
) {
	if err := db.AppendRunEvent(ctx, store.RunEvent{
		RunID:    event.RunID,
		IntentID: event.IntentID,
		State:    event.State,
		Action:   event.Action,
		Args:     event.Args,
		Result:   event.Result,
		Error:    event.Error,
	}); err != nil {
		log.Warn().Err(err).Str("run_id", event.RunID).Msg("failed to persist run event")
	}
}

func cancelLatestWaitingRun(
	ctx context.Context,
	intentID string,
	db *store.Store,
	log zerolog.Logger,
) {
	runState, _, err := db.LoadLatestWaitingRun(ctx, intentID)
	if err != nil {
		return
	}
	if runState.RunID == "" || runState.Status != "waiting" {
		return
	}
	if err := db.FinishRun(context.WithoutCancel(ctx), runState.RunID, "cancelled", "stopped by user"); err != nil {
		log.Warn().Err(err).Str("run_id", runState.RunID).Msg("failed to cancel waiting run")
	}
}
