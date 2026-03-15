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
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) error {
	if intent.WorkflowKind() == orchestrator.WorkflowTypeStarlark {
		return executeStarlarkRun(ctx, intent, runID, reg, db, log)
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
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) error {
	it := interpreter.NewStarlark(reg, log).
		WithOnChange(func(ctx context.Context, runID, intentID, state string, mem map[string]any) {
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
						Sequence: entry.Sequence,
						Kind:     entry.Kind,
						Name:     entry.Name,
						Args:     entry.Args,
						Result:   entry.Result,
						Error:    entry.Error,
					})
				}
				return converted, nil
			},
			func(ctx context.Context, runID, intentID string, entry interpreter.ReplayEntry) error {
				return db.AppendReplayHistory(ctx, store.ReplayHistoryEntry{
					Sequence: entry.Sequence,
					RunID:    runID,
					IntentID: intentID,
					Kind:     entry.Kind,
					Name:     entry.Name,
					Args:     entry.Args,
					Result:   entry.Result,
					Error:    entry.Error,
				})
			},
		)

	err := it.Execute(ctx, intent, "", interpreter.RunOptions{RunID: runID})
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
