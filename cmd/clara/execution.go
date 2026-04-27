package main

import (
	"context"
	"encoding/json"
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

	// 2. Setup a host-side context for the plugin to use
	ctxProvider := &registryContext{
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

	err = nativeIntent.Execute(name, ctxProvider)
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

type registryContext struct {
	ctx context.Context
	reg *registry.Registry
	log zerolog.Logger
}

func (c *registryContext) Shell() (contract.ShellIntegration, error) {
	return &registryShell{ctx: c.ctx, reg: c.reg, log: c.log}, nil
}

func (c *registryContext) FS() (contract.FSIntegration, error) {
	return nil, errors.New("FS integration not implemented in registry")
}

func (c *registryContext) DB() (contract.DBIntegration, error) {
	return &registryDB{ctx: c.ctx, reg: c.reg, log: c.log}, nil
}

func (c *registryContext) Chrome() (contract.ChromeIntegration, error) {
	return &registryChrome{ctx: c.ctx, reg: c.reg, log: c.log}, nil
}

type registryShell struct {
	ctx context.Context
	reg *registry.Registry
	log zerolog.Logger
}

func (s *registryShell) Configure(config []byte) error { return nil }

func (s *registryShell) Description() (string, error) {
	return "Host-side registry shell integration", nil
}

func (s *registryShell) Tools() ([]byte, error) { return nil, nil }

func (s *registryShell) CallTool(name string, args []byte) ([]byte, error) { return nil, nil }

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

type registryDB struct {
	ctx context.Context
	reg *registry.Registry
	log zerolog.Logger
}

func (d *registryDB) Configure(config []byte) error { return nil }
func (d *registryDB) Description() (string, error) {
	return "Host-side registry db integration", nil
}
func (d *registryDB) Tools() ([]byte, error) { return nil, nil }
func (d *registryDB) CallTool(name string, args []byte) ([]byte, error) { return nil, nil }

func (d *registryDB) Query(sql string, params []any) ([]map[string]any, error) {
	d.log.Debug().Str("sql", sql).Msg("native intent requested db query")
	res, err := d.reg.Call(d.ctx, "db.query", map[string]any{
		"sql":    sql,
		"params": params,
	})
	if err != nil {
		return nil, err
	}
	if results, ok := res.([]map[string]any); ok {
		return results, nil
	}
	// Attempt to convert if it's not exactly the right type (could be []any of map[string]any)
	if slice, ok := res.([]any); ok {
		var results []map[string]any
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok {
				results = append(results, m)
			}
		}
		return results, nil
	}
	return nil, fmt.Errorf("unexpected query result type: %T", res)
}

func (d *registryDB) Exec(sql string, params []any) (int64, error) {
	d.log.Debug().Str("sql", sql).Msg("native intent requested db exec")
	res, err := d.reg.Call(d.ctx, "db.exec", map[string]any{
		"sql":    sql,
		"params": params,
	})
	if err != nil {
		return 0, err
	}
	if m, ok := res.(map[string]any); ok {
		if rows, ok := m["rows_affected"].(int64); ok {
			return rows, nil
		}
		if rows, ok := m["rows_affected"].(float64); ok {
			return int64(rows), nil
		}
	}
	return 0, nil
}

func (d *registryDB) VecSearch(table string, vector []float32, limit int, minScore float64) ([]map[string]any, error) {
	d.log.Debug().Str("table", table).Msg("native intent requested db vec_search")
	res, err := d.reg.Call(d.ctx, "db.vec_search", map[string]any{
		"table":     table,
		"vector":    vector,
		"limit":     limit,
		"min_score": minScore,
	})
	if err != nil {
		return nil, err
	}
	if slice, ok := res.([]any); ok {
		var results []map[string]any
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok {
				results = append(results, m)
			}
		}
		return results, nil
	}
	return nil, fmt.Errorf("unexpected vec_search result type: %T", res)
}

func (d *registryDB) StageRows(table string, rows []any, replace bool) (int, error) {
	d.log.Debug().Str("table", table).Int("rows", len(rows)).Msg("native intent requested db stage_rows")
	res, err := d.reg.Call(d.ctx, "db.stage_rows", map[string]any{
		"table":   table,
		"rows":    rows,
		"replace": replace,
	})
	if err != nil {
		return 0, err
	}
	if m, ok := res.(map[string]any); ok {
		if inserted, ok := m["rows_inserted"].(int); ok {
			return inserted, nil
		}
		if inserted, ok := m["rows_inserted"].(float64); ok {
			return int(inserted), nil
		}
	}
	return 0, nil
}

type registryChrome struct {
	ctx context.Context
	reg *registry.Registry
	log zerolog.Logger
}

func (c *registryChrome) Configure(config []byte) error { return nil }
func (c *registryChrome) Description() (string, error) {
	return "Host-side registry chrome integration", nil
}
func (c *registryChrome) Tools() ([]byte, error) { return nil, nil }
func (c *registryChrome) CallTool(name string, args []byte) ([]byte, error) {
	c.log.Debug().Str("tool", name).Msg("native intent requested chrome tool call")
	var params map[string]any
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	res, err := c.reg.Call(c.ctx, "chrome."+name, params)
	if err != nil {
		return nil, err
	}

	return json.Marshal(res)
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
