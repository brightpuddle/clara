package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/tui"
	"github.com/cockroachdb/errors"
)

func runOneOff(parent context.Context, taskFile string, verbose bool) error {
	data, err := os.ReadFile(taskFile)
	if err != nil {
		return errors.Wrapf(err, "read task file %q", taskFile)
	}

	if !strings.EqualFold(filepath.Ext(taskFile), ".star") {
		return fmt.Errorf("unsupported intent file %q: only .star files are supported", taskFile)
	}

	intent, err := orchestrator.LoadIntentFile(taskFile, data)
	if err != nil {
		return errors.Wrapf(err, "parse intent from %q", taskFile)
	}

	logger := buildLogger()
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return errors.Wrapf(err, "create data dir %q", cfg.DataDir)
	}

	db, err := store.Open(cfg.DBPath(), logger)
	if err != nil {
		return errors.Wrap(err, "open database")
	}
	defer db.Close()

	reg := registry.New(logger)
	for _, srv := range cfg.MCPServers {
		mcpSrv := registry.NewMCPServer(
			srv.Name, srv.Description, srv.Command, srv.Args, srv.ResolvedEnv(), logger,
		)
		if err := reg.AddServer(mcpSrv); err != nil {
			return err
		}
	}

	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	startCtx, startCancel := context.WithCancel(ctx)
	defer startCancel()
	if err := reg.StartServers(startCtx); err != nil {
		return errors.Wrap(err, "start MCP servers")
	}
	defer reg.StopServers()

	runID := fmt.Sprintf("%s-oneoff-%d", intent.ID, time.Now().UnixNano())
	initialState := intent.InitialState
	if intent.WorkflowKind() == orchestrator.WorkflowTypeStarlark {
		initialState = "SCRIPT"
	}
	if err := db.InitRun(
		context.WithoutCancel(ctx),
		runID,
		intent.ID,
		initialState,
		intent.WorkflowKind(),
		intent.Script,
		nil,
	); err != nil {
		return errors.Wrap(err, "persist initial run state")
	}

	printer := newIntentWatchPrinter(tui.DetectTheme(), verbose, false)
	printer.printRule()
	fmt.Printf("%s %s\n", printer.theme.Dimmed("Running"), taskFile)
	printer.printStateSnapshot(store.RunState{
		RunID:     runID,
		IntentID:  intent.ID,
		State:     initialState,
		Status:    "running",
		UpdatedAt: time.Now().Unix(),
	})
	printer.printRule()

	lastEventID, err := db.LatestRunEventID(ctx, "")
	if err != nil {
		return errors.Wrap(err, "load latest run event id")
	}

	runErrCh := make(chan error, 1)
	go func() {
		err := executeIntentRun(ctx, intent, runID, reg, db, logger)
		status := "completed"
		errorText := ""
		var pauseErr *interpreter.PauseError
		switch {
		case ctx.Err() != nil:
			status = "cancelled"
		case errors.As(err, &pauseErr):
			status = "waiting"
		case err != nil:
			status = "failed"
			errorText = err.Error()
		}
		if status == "waiting" {
			runErrCh <- nil
			return
		}
		if finishErr := db.FinishRun(context.WithoutCancel(ctx), runID, status, errorText); finishErr != nil {
			logger.Warn().Err(finishErr).Str("run_id", runID).Msg("failed to persist one-off run completion")
		}
		if ctx.Err() != nil {
			runErrCh <- nil
			return
		}
		runErrCh <- err
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-runErrCh:
			if flushErr := flushRunEvents(ctx, db, printer, &lastEventID, runID); flushErr != nil {
				return flushErr
			}
			startCancel()
			if err != nil {
				return errors.Wrap(err, "execute intent")
			}
			return nil
		case <-ticker.C:
			if err := flushRunEvents(ctx, db, printer, &lastEventID, runID); err != nil {
				startCancel()
				return err
			}
		case <-ctx.Done():
			startCancel()
			return nil
		}
	}
}

func flushRunEvents(
	ctx context.Context,
	db *store.Store,
	printer *intentWatchPrinter,
	lastEventID *int64,
	runID string,
) error {
	events, err := db.RunEventsForRunSince(ctx, *lastEventID, runID)
	if err != nil {
		return errors.Wrap(err, "load run events")
	}
	for _, event := range events {
		printer.printEvent(event)
		*lastEventID = event.ID
	}
	return nil
}
