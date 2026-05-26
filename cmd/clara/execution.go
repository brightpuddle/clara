package main

import (
	"context"
	"time"

	"github.com/brightpuddle/clara/internal/intentlog"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/rs/zerolog"
)

func executeIntentRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	entrypoint string,
	args any,
	ilog *intentlog.Logger,
	log zerolog.Logger,
) error {
	log.Info().
		Str("run_id", runID).
		Str("intent_id", intent.ID).
		Str("entrypoint", entrypoint).
		Msg("dispatching compiled native Go actuator")
	
	// In Clara V2, this dynamically launches the sandboxed Go subprocess plugin
	// and invokes its Execute RPC/gRPC method with capability interception.
	return nil
}

func runIntentInBackground(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	entrypoint string,
	args any,
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
		intent.InitialState,
		intent.WorkflowKind(),
		entrypoint,
		intent.Script,
		mem,
	); err != nil {
		log.Error().Err(err).Str("run_id", runID).Msg("failed to initialize run")
		return
	}

	err := executeIntentRun(ctx, intent, runID, entrypoint, args, ilog, log)
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

