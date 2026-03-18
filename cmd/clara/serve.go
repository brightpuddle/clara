package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
	"github.com/sourcegraph/conc"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

var serveDaemon bool

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Clara agent",
	Long: `Start the Clara agent.

Without -d, the agent runs in the foreground of the current terminal — useful
for development or running under an external process supervisor.

With -d, the agent is started as a background macOS LaunchAgent via launchctl.
This is equivalent to 'clara agent start'.`,
	RunE:         runServe,
	SilenceUsage: true,
}

func init() {
	serveCmd.Flags().
		BoolVarP(&serveDaemon, "daemon", "d", false, "run as a background launchd agent")
}

func runServe(cmd *cobra.Command, args []string) error {
	if serveDaemon {
		return runDaemonize(cmd.Context())
	}

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return errors.Wrapf(err, "create data dir %q", cfg.DataDir)
	}

	logger := buildDaemonLogger()

	logger.Info().
		Str("data_dir", cfg.DataDir).
		Str("log_level", cfg.LogLevelNormalized()).
		Msg("clara agent starting")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := runDaemon(ctx, logger); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info().Msg("clara agent stopped")
	return nil
}

func runDaemon(ctx context.Context, logger zerolog.Logger) error {
	daemonCtx, shutdown := context.WithCancel(ctx)
	defer shutdown()

	db, err := store.Open(cfg.DBPath(), logger)
	if err != nil {
		return errors.Wrap(err, "open database")
	}
	defer db.Close()

	reg := registry.New(logger)

	for _, srv := range cfg.MCPServers {
		mcpSrv := registry.NewMCPServer(
			srv.Name,
			srv.Description,
			srv.Command,
			srv.Args,
			srv.ResolvedEnv(),
			cfg.MCPCommandSearchPathList(),
			logger,
		)
		if err := reg.AddServer(mcpSrv); err != nil {
			return err
		}
	}

	sup := supervisor.New(cfg.TasksDir(), reg, func(
		runCtx context.Context,
		intent *orchestrator.Intent,
		runID string,
		entrypoint string,
		args any,
	) error {
		if err := db.InitRun(
			context.WithoutCancel(runCtx),
			runID,
			intent.ID,
			initialRunState(intent),
			intent.WorkflowKind(),
			entrypoint,
			intent.Script,
			nil,
		); err != nil {
			return errors.Wrap(err, "initialize intent run")
		}
		return executeIntentRun(runCtx, intent, runID, entrypoint, args, reg, db, logger)
	}, logger).
		WithOnRunFinished(func(ctx context.Context, runID, intentID, status, errorText string) {
			if status == "waiting" {
				return
			}
			if err := db.FinishRun(ctx, runID, status, errorText); err != nil {
				logger.Warn().Err(err).Str("run_id", runID).Msg("failed to persist run completion")
			}
		})
	attachServer := registry.NewDynamicAttachServer(cfg.DynamicMCPSocketPath(), reg, logger)

	handler := buildHandler(reg, sup, attachServer, db, logger, shutdown)
	controlServer := ipc.NewServer(cfg.ControlSocketPath(), handler, logger)

	return runDaemonServices(daemonCtx, daemonServiceHooks{
		startServers: reg.StartServers,
		stopServers:  reg.StopServers,
		startControl: controlServer.ListenAndServe,
		startAttach:  attachServer.ListenAndServe,
		startSupervisor: func(ctx context.Context) error {
			return sup.Start(ctx)
		},
	}, logger)
}

type daemonServiceHooks struct {
	startServers    func(context.Context) error
	stopServers     func()
	startControl    func(context.Context) error
	startAttach     func(context.Context) error
	startSupervisor func(context.Context) error
}

func runDaemonServices(ctx context.Context, hooks daemonServiceHooks, logger zerolog.Logger) error {
	if err := hooks.startServers(ctx); err != nil {
		return errors.Wrap(err, "start MCP servers")
	}

	wg := conc.NewWaitGroup()

	wg.Go(func() {
		<-ctx.Done()
		if hooks.stopServers != nil {
			hooks.stopServers()
		}
	})

	wg.Go(func() {
		if err := hooks.startControl(ctx); err != nil {
			logger.Error().Err(err).Msg("control server error")
		}
	})

	wg.Go(func() {
		if err := hooks.startAttach(ctx); err != nil {
			logger.Error().Err(err).Msg("dynamic MCP attach server error")
		}
	})

	wg.Go(func() {
		if err := hooks.startSupervisor(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error().Err(err).Msg("supervisor error")
		}
	})

	wg.Wait()
	return nil
}

func buildHandler(
	reg *registry.Registry,
	sup *supervisor.Supervisor,
	attach *registry.DynamicAttachServer,
	db *store.Store,
	log zerolog.Logger,
	shutdown func(),
) ipc.HandlerFunc {
	return func(ctx context.Context, req *ipc.Request) *ipc.Response {
		switch req.Method {
		case ipc.MethodShutdown:
			if shutdown != nil {
				go shutdown()
			}
			return &ipc.Response{Message: "shutdown initiated"}

		case ipc.MethodStatus:
			intents := sup.IntentInfos()
			active := 0
			for _, intent := range intents {
				if intent.Active {
					active++
				}
			}
			return &ipc.Response{
				Message: "running",
				Data: map[string]any{
					"servers":        reg.ActiveServerCount(),
					"intents":        len(intents),
					"active_intents": active,
					"tools":          len(reg.Names()),
					"dynamic_mcp":    len(reg.DynamicServerNames()),
				},
			}

		case ipc.MethodList:
			intents := sup.IntentInfos()
			type taskEntry struct {
				IntentID    string `json:"intent_id"`
				Description string `json:"description,omitempty"`
				Handler     string `json:"handler"`
				Mode        string `json:"mode"`
				Schedule    string `json:"schedule,omitempty"`
				Interval    string `json:"interval,omitempty"`
				Trigger     string `json:"trigger,omitempty"`
				Active      bool   `json:"active"`
			}
			var list []taskEntry
			for _, intent := range intents {
				for _, task := range intent.Tasks {
					list = append(list, taskEntry{
						IntentID:    intent.ID,
						Description: intent.Description,
						Handler:     task.Handler,
						Mode:        task.Mode,
						Schedule:    task.Schedule,
						Interval:    task.Interval,
						Trigger:     task.Trigger,
						Active:      intent.Active,
					})
				}
			}
			if list == nil {
				list = []taskEntry{}
			}
			return &ipc.Response{Data: list}

		case ipc.MethodStart:
			id, _ := req.Params["id"].(string)
			if id == "" {
				return &ipc.Response{Error: "missing intent id"}
			}
			intent, ok := sup.Intent(id)
			if !ok {
				return &ipc.Response{Error: "intent " + id + " not found"}
			}
			taskName, _ := req.Params["task"].(string)

			// --input delivers JSON to a waiting run rather than starting a new one.
			if input, ok := req.Params["input"]; ok {
				runState, _, err := db.LoadLatestWaitingRun(ctx, id)
				if err != nil || runState.RunID == "" {
					return &ipc.Response{
						Error: "intent " + id + " has no waiting run to receive input",
					}
				}
				go resumeIntentByIDInBackground(ctx, intent, input, reg, db, log)
				return &ipc.Response{Message: "delivered input to waiting intent " + id}
			}

			// Dispatch by task mode: on-demand fires a single run; auto tasks
			// (schedule/worker/event) activate the persistent loop.
			isOnDemand := intentTaskIsOnDemand(intent, taskName)
			if isOnDemand {
				go runIntentInBackground(ctx, intent, taskName, reg, db, log)
				if taskName != "" {
					return &ipc.Response{Message: "intent " + id + " task " + taskName + " started"}
				}
				return &ipc.Response{Message: "intent " + id + " started"}
			}
			if err := sup.StartIntent(id, taskName); err != nil {
				return &ipc.Response{Error: err.Error()}
			}
			if taskName != "" {
				return &ipc.Response{Message: "intent " + id + " task " + taskName + " started"}
			}
			return &ipc.Response{Message: "intent " + id + " started"}

		case ipc.MethodStop:
			id, _ := req.Params["id"].(string)
			if id == "" {
				return &ipc.Response{Error: "missing intent id"}
			}
			taskName, _ := req.Params["task"].(string)
			if err := sup.StopIntent(id, taskName); err != nil {
				return &ipc.Response{Error: err.Error()}
			}
			cancelLatestWaitingRun(ctx, id, db, log)
			if taskName != "" {
				return &ipc.Response{Message: "intent " + id + " task " + taskName + " stopped"}
			}
			return &ipc.Response{Message: "intent " + id + " stopped"}

		case ipc.MethodToolList:
			filter, _ := req.Params["filter"].(string)
			view, _ := req.Params["view"].(string)
			if filter == "" && view != "tools" {
				caps := reg.AllCapabilities()
				result := make([]map[string]any, 0, len(caps))
				for _, cap := range caps {
					result = append(result, map[string]any{
						"name":        cap.Name,
						"description": cap.Description,
					})
				}
				return &ipc.Response{Data: result}
			}
			tools := filterTools(reg.Tools(), filter)
			result := make([]map[string]any, len(tools))
			for i, tool := range tools {
				result[i] = serializeToolInfo(tool)
			}
			return &ipc.Response{Data: result}

		case ipc.MethodToolShow:
			name, _ := req.Params["name"].(string)
			if name == "" {
				return &ipc.Response{Error: "missing name parameter"}
			}
			tool, ok := reg.Tool(name)
			if !ok {
				return &ipc.Response{Error: "tool " + name + " not found"}
			}
			return &ipc.Response{Data: serializeToolInfo(tool)}

		case ipc.MethodToolCall:
			name, _ := req.Params["name"].(string)
			if name == "" {
				return &ipc.Response{Error: "missing name parameter"}
			}

			args := map[string]any{}
			if rawArgs, ok := req.Params["args"]; ok && rawArgs != nil {
				parsedArgs, ok := rawArgs.(map[string]any)
				if !ok {
					return &ipc.Response{Error: "args parameter must be an object"}
				}
				args = parsedArgs
			}

			result, err := reg.Call(ctx, name, args)
			if err != nil {
				return &ipc.Response{Error: err.Error()}
			}
			return &ipc.Response{Data: result}

		case ipc.MethodMCPRegister:
			name, _ := req.Params["name"].(string)
			if name == "" {
				return &ipc.Response{Error: "missing name parameter"}
			}
			registration, err := attach.Register(name)
			if err != nil {
				return &ipc.Response{Error: err.Error()}
			}
			return &ipc.Response{
				Message: "dynamic MCP registration created",
				Data:    registration,
			}

		case ipc.MethodMCPUnregister:
			name, _ := req.Params["name"].(string)
			if name == "" {
				return &ipc.Response{Error: "missing name parameter"}
			}
			if err := attach.Unregister(name); err != nil {
				return &ipc.Response{Error: err.Error()}
			}
			return &ipc.Response{Message: "dynamic MCP registration removed"}

		case ipc.MethodMCPList:
			return &ipc.Response{Data: map[string]any{
				"active":  reg.DynamicServerNames(),
				"pending": attach.Registrations(),
			}}

		default:
			return &ipc.Response{Error: "unknown method: " + req.Method}
		}
	}
}

func runIntentInBackground(
	ctx context.Context,
	intent *orchestrator.Intent,
	entrypoint string,
	reg *registry.Registry,
	db *store.Store,
	log zerolog.Logger,
) {
	runID := fmt.Sprintf("%s-manual-%d", intent.ID, time.Now().UnixNano())
	if err := db.InitRun(
		context.WithoutCancel(ctx),
		runID,
		intent.ID,
		initialRunState(intent),
		intent.WorkflowKind(),
		entrypoint,
		intent.Script,
		nil,
	); err != nil {
		log.Warn().Err(err).Str("run_id", runID).Msg("failed to initialize manual run")
		return
	}
	err := executeIntentRun(ctx, intent, runID, entrypoint, nil, reg, db, log)
	if err != nil {
		var pauseErr *interpreter.PauseError
		if errors.As(err, &pauseErr) {
			log.Info().Str("intent_id", intent.ID).Str("run_id", runID).Msg("manual intent paused")
			return
		}
		if finishErr := db.FinishRun(context.WithoutCancel(ctx), runID, "failed", err.Error()); finishErr != nil {
			log.Warn().
				Err(finishErr).
				Str("run_id", runID).
				Msg("failed to persist manual run failure")
		}
		log.Error().Err(err).Str("intent_id", intent.ID).Msg("manual intent run error")
		return
	}
	if finishErr := db.FinishRun(context.WithoutCancel(ctx), runID, "completed", ""); finishErr != nil {
		log.Warn().
			Err(finishErr).
			Str("run_id", runID).
			Msg("failed to persist manual run completion")
	}
}

func initialRunState(intent *orchestrator.Intent) string {
	if intent.WorkflowKind() == orchestrator.WorkflowTypeStarlark {
		return "SCRIPT"
	}
	return intent.InitialState
}

// intentTaskIsOnDemand reports whether the target task for a start request is
// on-demand. If taskName is empty, it returns true only when every task in the
// intent is on-demand (i.e. there are no auto tasks to activate).
func intentTaskIsOnDemand(intent *orchestrator.Intent, taskName string) bool {
	if taskName != "" {
		for _, t := range intent.Tasks {
			if t.Handler == taskName {
				return t.Mode == "" || t.Mode == orchestrator.IntentModeOnDemand
			}
		}
		// Named task not found — let StartIntent return the appropriate error.
		return false
	}
	for _, t := range intent.Tasks {
		if t.Mode != "" && t.Mode != orchestrator.IntentModeOnDemand {
			return false
		}
	}
	return true
}

func buildLogger() zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevelNormalized())
	if err != nil {
		level = zerolog.InfoLevel
	}
	if isTerminalFile(os.Stderr) {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			Level(level).
			With().Timestamp().Logger()
	}
	return zerolog.New(os.Stdout).Level(level).With().Timestamp().Logger()
}

func buildDaemonLogger() zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevelNormalized())
	if err != nil {
		level = zerolog.InfoLevel
	}
	if isTerminalFile(os.Stderr) {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			Level(level).
			With().Timestamp().Logger()
	}

	writer := &lumberjack.Logger{
		Filename:   cfg.LogPath(),
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   false,
	}
	console := zerolog.ConsoleWriter{
		Out:        writer,
		NoColor:    true,
		TimeFormat: time.RFC3339,
	}
	return zerolog.New(console).Level(level).With().Timestamp().Logger()
}

func filterTools(tools []registry.ToolInfo, filter string) []registry.ToolInfo {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return tools
	}

	prefix := strings.TrimSuffix(filter, ".")
	if prefix == "" {
		return tools
	}
	if !strings.Contains(prefix, ".") {
		prefix += "."
	}

	filtered := make([]registry.ToolInfo, 0, len(tools))
	for _, tool := range tools {
		if strings.HasPrefix(tool.Name, prefix) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func serializeToolInfo(info registry.ToolInfo) map[string]any {
	entry := map[string]any{
		"name":        info.Name,
		"description": info.Description,
	}

	params := extractParams(info.Spec)
	if len(params) > 0 {
		entry["parameters"] = params
	}
	if len(info.Examples) > 0 {
		entry["examples"] = info.Examples
	}

	return entry
}

func extractParams(spec mcp.Tool) []map[string]any {
	schema := spec.InputSchema
	params := make([]map[string]any, 0, len(schema.Properties))
	required := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		required[name] = true
	}

	for name, prop := range schema.Properties {
		entry := map[string]any{
			"name":     name,
			"required": required[name],
		}
		if m, ok := prop.(map[string]any); ok {
			if typ, ok := m["type"].(string); ok {
				entry["type"] = typ
			}
			if desc, ok := m["description"].(string); ok {
				entry["description"] = desc
			}
		}
		params = append(params, entry)
	}

	sort.Slice(params, func(i, j int) bool {
		leftRequired, _ := params[i]["required"].(bool)
		rightRequired, _ := params[j]["required"].(bool)
		if leftRequired != rightRequired {
			return leftRequired
		}
		leftName, _ := params[i]["name"].(string)
		rightName, _ := params[j]["name"].(string)
		return leftName < rightName
	})

	return params
}
