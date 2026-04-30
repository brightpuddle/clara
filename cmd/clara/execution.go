package main

import (
	"context"
	"time"

	"github.com/brightpuddle/clara/internal/intentlog"
	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/rs/zerolog"
)

func executeIntentRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	entrypoint string,
	args any,
	reg *registry.Registry,
	ilog *intentlog.Logger,
	log zerolog.Logger,
) error {
	switch intent.WorkflowKind() {
	case orchestrator.WorkflowTypeStarlark:
		return executeStarlarkRun(ctx, intent, runID, entrypoint, args, reg, ilog, log)
	default:
		return executeStateMachineRun(ctx, intent, runID, reg, ilog, log)
	}
}

func executeStarlarkRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	entrypoint string,
	args any,
	reg *registry.Registry,
	ilog *intentlog.Logger,
	log zerolog.Logger,
) error {
	it := interpreter.NewStarlark(reg, log).
		WithOnStep(func(ctx context.Context, event interpreter.StepEvent) {
			appendRunEvent(ilog, log, event)
		})
	return it.Execute(ctx, intent, "", interpreter.RunOptions{
		RunID:       runID,
		Entrypoint:  entrypoint,
		HandlerArgs: args,
	})
}

func executeStateMachineRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	reg *registry.Registry,
	ilog *intentlog.Logger,
	log zerolog.Logger,
) error {
	it := interpreter.New(reg, log).WithOnStep(func(ctx context.Context, event interpreter.StepEvent) {
		appendRunEvent(ilog, log, event)
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
	ilog *intentlog.Logger,
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

	err := executeIntentRun(ctx, intent, runID, entrypoint, args, reg, ilog, log)
	if err != nil {
		if finishErr := db.FinishRun(context.WithoutCancel(ctx), runID, "failed", err.Error()); finishErr != nil {
			log.Warn().
				Err(finishErr).
				Str("run_id", runID).
				Msg("failed to persist run failure")
		}
		appendFinishEvent(ilog, log, runID, intent.ID, entrypoint, "failed", err.Error())
		return
	}

	if finishErr := db.FinishRun(context.WithoutCancel(ctx), runID, "completed", ""); finishErr != nil {
		log.Warn().
			Err(finishErr).
			Str("run_id", runID).
			Msg("failed to persist run completion")
	}
	appendFinishEvent(ilog, log, runID, intent.ID, entrypoint, "completed", "")
}

func initialRunState(intent *orchestrator.Intent) string {
	if intent.WorkflowKind() == orchestrator.WorkflowTypeStarlark {
		return starlarkScriptState
	}
	return intent.InitialState
}

func appendRunEvent(
	ilog *intentlog.Logger,
	log zerolog.Logger,
	event interpreter.StepEvent,
) {
	if err := ilog.Append(intentlog.Event{
		Time:       time.Now(),
		RunID:      event.RunID,
		IntentID:   event.IntentID,
		Entrypoint: event.Entrypoint,
		State:      event.State,
		Action:     event.Action,
		Args:       event.Args,
		Result:     event.Result,
		Error:      event.Error,
	}); err != nil {
		log.Warn().Err(err).Str("run_id", event.RunID).Msg("failed to write intent event")
	}
}

func appendFinishEvent(
	ilog *intentlog.Logger,
	log zerolog.Logger,
	runID, intentID, entrypoint, status, errorText string,
) {
	if err := ilog.Append(intentlog.Event{
		Time:       time.Now(),
		RunID:      runID,
		IntentID:   intentID,
		Entrypoint: entrypoint,
		Action:     "finish",
		Result:     map[string]any{"status": status},
		Error:      errorText,
	}); err != nil {
		log.Warn().Err(err).Str("run_id", runID).Msg("failed to write finish event")
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

// starlarkScriptState is the state name used when tracking Starlark workflow runs.
const starlarkScriptState = "SCRIPT"
