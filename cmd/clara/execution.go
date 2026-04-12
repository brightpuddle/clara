package main

import (
	"context"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/cockroachdb/errors"
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
	if intent.WorkflowKind() == orchestrator.WorkflowTypeStarlark {
		return executeStarlarkRun(ctx, intent, runID, entrypoint, args, reg, db, log)
	}
	return executeStateMachineRun(ctx, intent, runID, reg, db, log)
}

func executeStateMachineRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) error {
	it := interpreter.New(reg, log).
		WithOnChange(func(ctx context.Context, runID, intentID, state string, mem map[string]any) {
			if err := db.SaveRunState(ctx, runID, intentID, state, mem); err != nil {
				log.Warn().Err(err).Str("run_id", runID).Msg("failed to persist run state")
			}
		}).
		WithOnStep(func(ctx context.Context, event interpreter.StepEvent) {
			appendRunEvent(ctx, db, log, event)
		})
	return it.Execute(ctx, intent, intent.InitialState, interpreter.RunOptions{RunID: runID})
}

func executeStarlarkRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	entrypoint string,
	args any,
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) error {
	it := interpreter.NewStarlark(reg, log)
	if cfg != nil {
		it = it.WithMCPTimeout(cfg.MCPStartupTimeout)
	}
	it = it.WithOnChange(func(ctx context.Context, runID, intentID, state string, mem map[string]any) {
		if err := db.SaveRunState(ctx, runID, intentID, state, mem); err != nil {
			log.Warn().Err(err).Str("run_id", runID).Msg("failed to persist starlark run state")
		}
	}).
		WithOnStep(func(ctx context.Context, event interpreter.StepEvent) {
			appendRunEvent(ctx, db, log, event)
		}).
		WithHistory(
			func(ctx context.Context, runID string) ([]interpreter.ReplayEntry, error) {
				entries, err := db.LoadReplayHistory(ctx, runID)
				if err != nil {
					return nil, err
				}
				converted := make([]interpreter.ReplayEntry, 0, len(entries))
				for _, entry := range entries {
					converted = append(converted, interpreter.ReplayEntry{
						Sequence:   entry.Sequence,
						RunID:      entry.RunID,
						IntentID:   entry.IntentID,
						Entrypoint: entry.Entrypoint,
						Kind:       entry.Kind,
						Name:       entry.Name,
						Args:       entry.Args,
						Result:     entry.Result,
						Error:      entry.Error,
					})
				}
				return converted, nil
			},
			func(ctx context.Context, runID, intentID string, entry interpreter.ReplayEntry) error {
				return db.AppendReplayHistory(ctx, store.ReplayHistoryEntry{
					Sequence:   entry.Sequence,
					RunID:      runID,
					IntentID:   intentID,
					Entrypoint: entry.Entrypoint,
					Kind:       entry.Kind,
					Name:       entry.Name,
					Args:       entry.Args,
					Result:     entry.Result,
					Error:      entry.Error,
				})
			},
		)

	err := it.Execute(ctx, intent, "", interpreter.RunOptions{
		RunID:       runID,
		Entrypoint:  entrypoint,
		HandlerArgs: args,
	})
	var pauseErr *interpreter.PauseError
	if errors.As(err, &pauseErr) {
		if markErr := db.MarkRunWaiting(context.WithoutCancel(ctx), runID, pauseErr.Request.Name, pauseErr.Request.Args); markErr != nil {
			return errors.Wrap(markErr, "mark starlark run waiting")
		}
		appendRunEvent(context.WithoutCancel(ctx), db, log, interpreter.StepEvent{
			RunID:    runID,
			IntentID: intent.ID,
			State:    "SCRIPT",
			Action:   "wait." + pauseErr.Request.Name,
			Args:     pauseErr.Request.Args,
			Error:    "waiting for resume input",
		})
	}
	return err
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

func resumeIntentByIDInBackground(
	ctx context.Context,
	intent *orchestrator.Intent,
	input any,
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) {
	runState, _, err := db.LoadLatestWaitingRun(ctx, intent.ID)
	if err != nil {
		log.Error().
			Err(err).
			Str("intent_id", intent.ID).
			Msg("failed to load waiting run for trigger input")
		return
	}
	if runState.RunID == "" {
		log.Warn().Str("intent_id", intent.ID).Msg("no waiting run found for trigger input")
		return
	}
	if err := appendWaitInput(ctx, db, runState, input); err != nil {
		log.Error().Err(err).Str("run_id", runState.RunID).Msg("failed to append waiting input")
		return
	}
	if err := resumeStoredStarlarkRun(ctx, runState, reg, db, log); err != nil {
		log.Error().Err(err).Str("run_id", runState.RunID).Msg("failed to resume waiting run")
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

func appendWaitInput(
	ctx context.Context,
	db *store.Store,
	runState store.RunState,
	input any,
) error {
	history, err := db.LoadReplayHistory(ctx, runState.RunID)
	if err != nil {
		return errors.Wrap(err, "load replay history")
	}
	if err := db.AppendReplayHistory(ctx, store.ReplayHistoryEntry{
		RunID:      runState.RunID,
		IntentID:   runState.IntentID,
		Entrypoint: runState.Entrypoint,
		Sequence:   len(history),
		Kind:       "wait",
		Name:       runState.WaitName,
		Args:       runState.WaitArgs,
		Result:     input,
	}); err != nil {
		return errors.Wrap(err, "append wait result")
	}
	return nil
}

func resumeStoredStarlarkRun(
	ctx context.Context,
	runState store.RunState,
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) error {
	intent := &orchestrator.Intent{
		ID:           runState.IntentID,
		WorkflowType: orchestrator.WorkflowTypeStarlark,
		Script:       runState.ScriptSource,
	}
	if err := intent.Validate(); err != nil {
		return errors.Wrap(err, "validate stored starlark intent")
	}
	if err := db.InitRun(
		context.WithoutCancel(ctx),
		runState.RunID,
		runState.IntentID,
		"SCRIPT",
		orchestrator.WorkflowTypeStarlark,
		runState.Entrypoint,
		runState.ScriptSource,
		nil,
	); err != nil {
		return errors.Wrap(err, "reinitialize run")
	}

	err := executeIntentRun(ctx, intent, runState.RunID, runState.Entrypoint, nil, reg, db, log)
	var pauseErr *interpreter.PauseError
	switch {
	case ctx.Err() != nil:
		return nil
	case errors.As(err, &pauseErr):
		return nil
	case err != nil:
		if finishErr := db.FinishRun(context.WithoutCancel(ctx), runState.RunID, "failed", err.Error()); finishErr != nil {
			log.Warn().
				Err(finishErr).
				Str("run_id", runState.RunID).
				Msg("failed to persist resumed run failure")
		}
		return errors.Wrap(err, "resume workflow")
	default:
		if finishErr := db.FinishRun(context.WithoutCancel(ctx), runState.RunID, "completed", ""); finishErr != nil {
			log.Warn().
				Err(finishErr).
				Str("run_id", runState.RunID).
				Msg("failed to persist resumed run completion")
		}
		return nil
	}
}
