package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	dbbuiltin "github.com/brightpuddle/clara/internal/builtin/db"
	fsbuiltin "github.com/brightpuddle/clara/internal/builtin/fs"
	notifybuiltin "github.com/brightpuddle/clara/internal/builtin/notify"
	shellbuiltin "github.com/brightpuddle/clara/internal/builtin/shell"
	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/intentlog"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/loghub"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/server"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/toolcatalog"
	"github.com/brightpuddle/clara/internal/webui"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
	"github.com/sourcegraph/conc"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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

	ilog, err := intentlog.New(cfg.IntentLogsDir())
	if err != nil {
		return errors.Wrap(err, "open intent log dir")
	}
	defer ilog.Close()

	reg := registry.New(logger)

	// Register built-in tools directly (no subprocess).
	builtinCfg := func(key string) map[string]any {
		if cfg.Integrations != nil {
			return cfg.Integrations[key]
		}
		return nil
	}
	if err := shellbuiltin.Register(daemonCtx, builtinCfg("shell"), reg, logger); err != nil {
		logger.Error().Err(err).Msg("failed to register shell builtin")
	}
	if err := fsbuiltin.Register(daemonCtx, builtinCfg("fs"), reg, logger); err != nil {
		logger.Error().Err(err).Msg("failed to register fs builtin")
	}
	if err := dbbuiltin.Register(daemonCtx, builtinCfg("db"), reg, logger); err != nil {
		logger.Error().Err(err).Msg("failed to register db builtin")
	}

	// Register the notify builtin.
	if err := notifybuiltin.Register(daemonCtx, cfg.Notify, reg, logger); err != nil {
		logger.Error().Err(err).Msg("failed to register notify builtin")
	}

	// Register configured MCP servers.
	for _, srv := range cfg.MCPServers {
		mcpSrv := buildMCPServer(srv, logger)
		if err := reg.AddServer(mcpSrv); err != nil {
			logger.Error().Err(err).Str("name", srv.Name).Msg("failed to add MCP server")
		}
	}

	sup := supervisor.New(reg, func(
		runCtx context.Context,
		intent *orchestrator.Intent,
		runID string,
		entrypoint string,
		args any,
	) error {
		var mem map[string]any
		if m, ok := args.(map[string]any); ok {
			mem = m
		}
		if err := db.InitRun(
			context.WithoutCancel(runCtx),
			runID,
			intent.ID,
			intent.InitialState,
			intent.WorkflowKind(),
			entrypoint,
			intent.Script,
			mem,
		); err != nil {
			return errors.Wrap(err, "initialize intent run")
		}
		return executeIntentRun(runCtx, intent, runID, entrypoint, args, ilog, logger)
	}, logger).
		WithOnRunFinished(func(ctx context.Context, runID, intentID, status, errorText string) {
			if status == "waiting" {
				return
			}
			if err := db.FinishRun(ctx, runID, status, errorText); err != nil {
				logger.Warn().Err(err).Str("run_id", runID).Msg("failed to persist run completion")
			}
		})

	loader := newPluginLoader(reg, sup, cfg, logger)
	if err := loader.loadAll(); err != nil {
		logger.Error().Err(err).Msg("failed to load native plugins")
	}

	httpServer := server.New(cfg, reg, loader, logger)

	// Attach the web UI (served at /ui/ on the same port as the HTTP server).
	uiCfgPath := cfgFile
	if uiCfgPath == "" {
		uiCfgPath = config.DefaultConfigPath()
	}
	ui := webui.New(cfg, uiCfgPath, sup, reg, loader, ilog, logger)
	httpServer.WebUI = ui

	approvals := supervisor.NewApprovalStore()
	handler := &daemonHandler{
		base: buildHandler(reg, sup, db, ilog, loader, logger, shutdown, approvals),
		hub:  loghub.New(),
	}
	controlServer, err := ipc.NewServer(cfg.ControlSocketPath(), handler, logger)
	if err != nil {
		return errors.Wrap(err, "create control socket server")
	}

	builderDir := cfg.DataDir + "/workspace"
	builder, err := supervisor.NewBuilder(builderDir)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to create builder; evaluator builder mode disabled")
		builder = nil
	}
	evaluator := supervisor.NewEvaluator(logger, sup.EventBus(), supervisor.NoopLLMClient(), builder, cfg.DataDir+"/bin")

	return runDaemonServices(daemonCtx, daemonServiceHooks{
		startMCPServers: func(ctx context.Context) error {
			if err := reg.StartServers(ctx); err != nil {
				return err
			}
			waitCtx, waitCancel := context.WithTimeout(ctx, cfg.MCPStartupTimeout)
			defer waitCancel()
			_ = reg.WaitReady(waitCtx)
			return nil
		},
		stopMCPServers: reg.StopServers,
		startHTTPServer: func(ctx context.Context) error {
			return httpServer.Start()
		},
		stopHTTPServer: func() {
			_ = httpServer.Stop(context.Background())
		},
		startControl:   controlServer.ListenAndServe,
		startSupervisor: func(ctx context.Context) error {
			return sup.Start(ctx)
		},
		startEvaluator: func(ctx context.Context) error {
			sub, unsubscribe := sup.EventBus().SubscribeCloud()
			defer unsubscribe()
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case ce, ok := <-sub:
					if !ok {
						return nil
					}
					if err := evaluator.OnEvent(ctx, ce); err != nil {
						logger.Error().Err(err).
							Str("event_id", ce.ID).
							Str("event_type", ce.Type).
							Msg("evaluator error")
					}
				}
			}
		},
	}, logger)
}

type daemonServiceHooks struct {
	startMCPServers  func(context.Context) error
	stopMCPServers   func()
	startHTTPServer  func(context.Context) error
	stopHTTPServer   func()
	startControl     func(context.Context) error
	startSupervisor  func(context.Context) error
	startEvaluator   func(context.Context) error
}

func runDaemonServices(ctx context.Context, hooks daemonServiceHooks, logger zerolog.Logger) error {
	// Start MCP servers and wait for them to be ready before serving requests.
	if hooks.startMCPServers != nil {
		if err := hooks.startMCPServers(ctx); err != nil {
			return errors.Wrap(err, "start MCP servers")
		}
	}

	if hooks.startHTTPServer != nil {
		if err := hooks.startHTTPServer(ctx); err != nil {
			return errors.Wrap(err, "start HTTP server")
		}
	}

	wg := conc.NewWaitGroup()

	if hooks.stopMCPServers != nil {
		wg.Go(func() {
			<-ctx.Done()
			hooks.stopMCPServers()
		})
	}

	if hooks.stopHTTPServer != nil {
		wg.Go(func() {
			<-ctx.Done()
			hooks.stopHTTPServer()
		})
	}

	wg.Go(func() {
		if err := hooks.startControl(ctx); err != nil {
			logger.Error().Err(err).Msg("control server error")
		}
	})
	wg.Go(func() {
		if err := hooks.startSupervisor(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error().Err(err).Msg("supervisor error")
		}
	})

	if hooks.startEvaluator != nil {
		wg.Go(func() {
			if err := hooks.startEvaluator(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error().Err(err).Msg("evaluator error")
			}
		})
	}

	wg.Wait()
	return nil
}

func buildHandler(
	reg *registry.Registry,
	sup *supervisor.Supervisor,
	db *store.Store,
	ilog *intentlog.Logger,
	loader *pluginLoader,
	log zerolog.Logger,
	shutdown func(),
	approvals *supervisor.ApprovalStore,
) ipc.HandlerFunc {
	return func(ctx context.Context, req *ipc.Request, w ipc.ResponseWriter) {
		writeResp := func(resp *ipc.Response) {
			if err := w.Write(resp); err != nil {
				log.Debug().Err(err).Str("method", req.Method).Msg("failed to write response")
			}
		}

		switch req.Method {
		case ipc.MethodShutdown:
			if shutdown != nil {
				go shutdown()
			}
			writeResp(&ipc.Response{Message: "shutdown initiated"})

		case ipc.MethodStatus:
			// If an id is provided (even if empty string), return the active run states.
			// This is used by 'clara intent logs'.
			if id, ok := req.Params["id"].(string); ok {
				states, err := db.ActiveRunStates(ctx, id)
				if err != nil {
					writeResp(&ipc.Response{Error: err.Error()})
					return
				}
				writeResp(&ipc.Response{Data: states})
				return
			}

			// Otherwise return general agent status.
			intents := sup.IntentInfos()
			active := 0
			for _, intent := range intents {
				if intent.Active {
					active++
				}
			}
			writeResp(&ipc.Response{
				Message: "running",
				Data: map[string]any{
					"intents":        len(intents),
					"active_intents": active,
					"tools":          len(reg.Names()),
				},
			})

		case ipc.MethodList:
			intents := sup.IntentInfos()
			type taskEntry struct {
				IntentID    string         `json:"intent_id"`
				Path        string         `json:"path,omitempty"`
				Description string         `json:"description,omitempty"`
				Handler     string         `json:"handler"`
				Mode        string         `json:"mode"`
				Schedule    string         `json:"schedule,omitempty"`
				Interval    string         `json:"interval,omitempty"`
				Trigger     string         `json:"trigger,omitempty"`
				TriggerArgs map[string]any `json:"trigger_args,omitempty"`
				Active      bool           `json:"active"`
				Error       string         `json:"error,omitempty"`
			}
			var list []taskEntry
			for _, intent := range intents {
				if intent.Error != "" {
					list = append(list, taskEntry{
						IntentID:    intent.ID,
						Path:        intent.Path,
						Description: intent.Description,
						Error:       intent.Error,
					})
					continue
				}
				for _, task := range intent.Tasks {
					list = append(list, taskEntry{
						IntentID:    intent.ID,
						Path:        intent.Path,
						Description: intent.Description,
						Handler:     task.Handler,
						Mode:        task.Mode,
						Schedule:    task.Schedule,
						Interval:    task.Interval,
						Trigger:     task.Trigger,
						TriggerArgs: task.TriggerArgs,
						Active:      intent.Active,
					})
				}
			}
			if list == nil {
				list = []taskEntry{}
			}
			writeResp(&ipc.Response{Data: list})

		case ipc.MethodRun:
			path, _ := req.Params["path"].(string)
			if path == "" {
				writeResp(&ipc.Response{Error: "missing path parameter"})
				return
			}
			absPath, err := filepath.Abs(path)
			if err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			intent := &orchestrator.Intent{
				ID:           strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath)),
				WorkflowType: orchestrator.WorkflowTypeNative,
				Script:       absPath,
			}
			if strings.HasSuffix(absPath, ".star") {
				intent.WorkflowType = orchestrator.WorkflowTypeStarlark
				data, err := os.ReadFile(absPath)
				if err != nil {
					writeResp(&ipc.Response{Error: "read script file: " + err.Error()})
					return
				}
				intent.Script = string(data)
			} else if strings.HasSuffix(absPath, ".yaml") || strings.HasSuffix(absPath, ".yml") || strings.HasSuffix(absPath, ".json") {
				data, err := os.ReadFile(absPath)
				if err != nil {
					writeResp(&ipc.Response{Error: "read intent file: " + err.Error()})
					return
				}
				intent, err = orchestrator.ParseIntent(data)
				if err != nil {
					writeResp(&ipc.Response{Error: "parse intent: " + err.Error()})
					return
				}
			}
			
			runID := fmt.Sprintf("%s-oneoff-%d", intent.ID, time.Now().UnixNano())
			startedAt := time.Now()
			
			go runIntentInBackground(ctx, intent, runID, "main", nil, db, ilog, log)
			
			writeResp(&ipc.Response{
				Message: "intent " + intent.ID + " started",
				Data: map[string]any{
					"run_id":     runID,
					"intent_id":  intent.ID,
					"started_at": startedAt.Format(time.RFC3339Nano),
				},
			})

		case ipc.MethodStart:
			id, _ := req.Params["id"].(string)
			if id == "" {
				writeResp(&ipc.Response{Error: "missing intent id"})
				return
			}
			intent, ok := sup.Intent(id)
			if !ok {
				writeResp(&ipc.Response{Error: "intent " + id + " not found"})
				return
			}
			taskName, _ := req.Params["task"].(string)

			// Support arguments in either top-level Args or Params["args"]
			args := req.Args
			if args == nil {
				if a, ok := req.Params["args"].(map[string]any); ok {
					args = a
				}
			}

			// Dispatch by task mode: on-demand fires a single run; auto tasks
			// (schedule/worker/event) activate the persistent loop.
			isOnDemand := intentTaskIsOnDemand(intent, taskName)
			if isOnDemand {
				runID := fmt.Sprintf("%s-manual-%d", intent.ID, time.Now().UnixNano())
				startedAt := time.Now()
				go runIntentInBackground(ctx, intent, runID, taskName, args, db, ilog, log)
				msg := "intent " + id + " started"
				if taskName != "" {
					msg = "intent " + id + " task " + taskName + " started"
				}
				writeResp(&ipc.Response{
					Message: msg,
					Data: map[string]any{
						"run_id":     runID,
						"started_at": startedAt.Format(time.RFC3339Nano),
					},
				})
				return
			}
			if err := sup.StartIntent(id, taskName); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			if taskName != "" {
				writeResp(&ipc.Response{Message: "intent " + id + " task " + taskName + " started"})
			} else {
				writeResp(&ipc.Response{Message: "intent " + id + " started"})
			}

		case ipc.MethodStop:
			id, _ := req.Params["id"].(string)
			if id == "" {
				writeResp(&ipc.Response{Error: "missing intent id"})
				return
			}
			taskName, _ := req.Params["task"].(string)
			if err := sup.StopIntent(id, taskName); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			if taskName != "" {
				writeResp(&ipc.Response{Message: "intent " + id + " task " + taskName + " stopped"})
			} else {
				writeResp(&ipc.Response{Message: "intent " + id + " stopped"})
			}

		case ipc.MethodToolList:
			filter, _ := req.Params["filter"].(string)
			view, _ := req.Params["view"].(string)
			if filter == "" && view != "tools" {
				tools := reg.Tools()
				catalogTools := make([]toolcatalog.Tool, len(tools))
				for i, t := range tools {
					catalogTools[i] = toolcatalog.Tool{
						Name:        t.Name,
						Description: t.Description,
					}
				}
				providers := toolcatalog.ProviderSummariesFromTools(catalogTools)
				result := make([]map[string]any, 0, len(providers))
				for _, p := range providers {
					var desc string
					// Explicit namespace description (from config, built-in defaults, or native plugins)
					if nsDesc := reg.NamespaceDescription(p.Name); nsDesc != "" {
						desc = nsDesc
					}

					events := listEventTools(ctx, reg, p.Name)
					result = append(result, map[string]any{
						"name":        p.Name,
						"description": desc,
						"events":      events,
					})
				}
				writeResp(&ipc.Response{Data: result})
				return
			}
			// Filter out internal tools (clara_list_events is an implementation detail).
			var allTools []map[string]any

			seenServers := make(map[string]bool)
			for _, t := range reg.Tools() {
				if strings.HasSuffix(t.Name, ".clara_list_events") {
					continue
				}
				allTools = append(allTools, serializeToolInfo(t))
				if parts := strings.SplitN(t.Name, ".", 2); len(parts) == 2 {
					seenServers[parts[0]] = true
				}
			}

			// Add event tools for all known servers
			for serverName := range seenServers {
				allTools = append(allTools, listEventTools(ctx, reg, serverName)...)
			}

			// Now filter the combined list of tools and events
			var result []map[string]any
			for _, t := range allTools {
				name, _ := t["name"].(string)
				if filter == "" || strings.HasPrefix(name, filter) {
					result = append(result, t)
				}
			}
			writeResp(&ipc.Response{Data: result})

		case ipc.MethodToolShow:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			tool, ok := reg.Tool(name)
			if ok {
				writeResp(&ipc.Response{Data: serializeToolInfo(tool)})
				return
			}
			// Not a regular tool — check if it matches a known event tool.
			if parts := strings.SplitN(name, ".", 2); len(parts) == 2 {
				for _, et := range listEventTools(ctx, reg, parts[0]) {
					if et["name"] == name {
						writeResp(&ipc.Response{Data: et})
						return
					}
				}
			}
			writeResp(&ipc.Response{Error: "tool " + name + " not found"})

		case ipc.MethodToolCall:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}

			args := req.Args
			if args == nil {
				if rawArgs, ok := req.Params["args"]; ok && rawArgs != nil {
					parsedArgs, ok := rawArgs.(map[string]any)
					if !ok {
						writeResp(&ipc.Response{Error: "args parameter must be an object"})
						return
					}
					args = parsedArgs
				}
			}
			if args == nil {
				args = map[string]any{}
			}

			result, err := reg.Call(ctx, name, args)
			if err != nil {
				parts := strings.SplitN(name, ".", 2)
				if len(parts) == 2 {
					eventsList := listEventTools(ctx, reg, parts[0])
					isEvent := false
					for _, ev := range eventsList {
						if evName, ok := ev["name"].(string); ok && evName == name {
							isEvent = true
							break
						}
					}

					if isEvent {
						events, unsubscribe := sup.EventBus().Subscribe()
						defer unsubscribe()

						for {
							select {
							case <-ctx.Done():
								writeResp(&ipc.Response{Error: ctx.Err().Error()})
								return
							case event, ok := <-events:
								if !ok {
									writeResp(&ipc.Response{Error: "event bus closed"})
									return
								}
								if event.Server == parts[0] && event.Method == parts[1] {
									matched := true
									if len(args) > 0 {
										evParams, _ := event.Params.(map[string]any)
										for k, v := range args {
											if fmt.Sprintf("%v", evParams[k]) != fmt.Sprintf("%v", v) {
												matched = false
												break
											}
										}
									}
									if matched {
										writeResp(&ipc.Response{Data: event.Params})
										return
									}
								}
							}
						}
					}
				}

				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Data: result})

		case ipc.MethodEvents:
			events, unsubscribe := sup.EventBus().Subscribe()
			defer unsubscribe()

			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-events:
					if !ok {
						return
					}
					if err := w.Write(&ipc.Response{Data: event}); err != nil {
						return
					}
				}
			}

		case ipc.MethodPluginList:
			writeResp(&ipc.Response{Data: loader.List()})

		case ipc.MethodPluginLoad:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			if err := loader.Load(name); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "plugin " + name + " loaded"})

		case ipc.MethodPluginUnload:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			if err := loader.Unload(name); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "plugin " + name + " unloaded"})

		case ipc.MethodPluginReload:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			if err := loader.Reload(name); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "plugin " + name + " reloaded"})

		case ipc.MethodMCPList:
			statuses := reg.ServerStatuses()
			managed := make([]map[string]any, 0, len(statuses))
			for srvName, status := range statuses {
				managed = append(managed, map[string]any{
					"name":   srvName,
					"status": string(status),
				})
			}
			sort.Slice(managed, func(i, j int) bool {
				return managed[i]["name"].(string) < managed[j]["name"].(string)
			})
			writeResp(&ipc.Response{Data: map[string]any{
				"managed": managed,
				"active":  reg.DynamicServerNames(),
				"pending": []string{},
			}})

		case ipc.MethodMCPStart:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			if err := reg.StartServer(ctx, name); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "MCP server " + name + " started"})

		case ipc.MethodMCPStop:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			if err := reg.StopServer(name); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "MCP server " + name + " stopped"})

		case ipc.MethodMCPRestart:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			if err := reg.RestartServer(ctx, name); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "MCP server " + name + " restarted"})

		case ipc.MethodMCPAdd:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			command, _ := req.Params["command"].(string)
			url, _ := req.Params["url"].(string)
			if command == "" && url == "" {
				writeResp(&ipc.Response{Error: "either command or url is required"})
				return
			}
			overwrite, _ := req.Params["overwrite"].(bool)
			desc, _ := req.Params["description"].(string)
			token, _ := req.Params["token"].(string)
			skipVerify, _ := req.Params["skip_verify"].(bool)

			// Check if already exists in config.
			existingIdx := -1
			for i, srv := range cfg.MCPServers {
				if srv.Name == name {
					existingIdx = i
					break
				}
			}
			if existingIdx != -1 && !overwrite {
				writeResp(&ipc.Response{
					Error: "server " + name + " already exists; use overwrite:true to update",
				})
				return
			}

			newSrvCfg := config.MCPServerConfig{
				Name:        name,
				Command:     command,
				URL:         url,
				Description: desc,
				Token:       token,
				SkipVerify:  skipVerify,
			}
			if env, ok := req.Params["env"].(map[string]any); ok {
				newSrvCfg.Env = make(map[string]string)
				for k, v := range env {
					newSrvCfg.Env[k] = fmt.Sprint(v)
				}
			}

			if existingIdx != -1 {
				cfg.MCPServers[existingIdx] = newSrvCfg
			} else {
				cfg.MCPServers = append(cfg.MCPServers, newSrvCfg)
			}
			if err := saveConfig(cfg, log); err != nil {
				writeResp(&ipc.Response{Error: "failed to save config: " + err.Error()})
				return
			}

			// Update registry: remove old instance if present.
			if existingIdx != -1 || reg.HasServer(name) {
				_ = reg.RemoveServer(name)
			}
			mcpSrv := buildMCPServer(newSrvCfg, log)
			if err := reg.AddServer(mcpSrv); err != nil {
				writeResp(&ipc.Response{Error: "failed to add to registry: " + err.Error()})
				return
			}
			if err := reg.StartServer(ctx, name); err != nil {
				writeResp(&ipc.Response{Error: "added but failed to start: " + err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "MCP server " + name + " added and started"})

		case ipc.MethodMCPRemove:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			found := false
			for i, srv := range cfg.MCPServers {
				if srv.Name == name {
					cfg.MCPServers = append(cfg.MCPServers[:i], cfg.MCPServers[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				writeResp(&ipc.Response{Error: "server " + name + " not found in config"})
				return
			}
			if err := saveConfig(cfg, log); err != nil {
				writeResp(&ipc.Response{Error: "failed to save config: " + err.Error()})
				return
			}
			_ = reg.RemoveServer(name)
			writeResp(&ipc.Response{Message: "MCP server " + name + " removed from config and stopped"})

		case ipc.MethodApprovalList:
			list := approvals.List()
			writeResp(&ipc.Response{Data: list})

		case ipc.MethodApprovalShow:
			id, _ := req.Params["id"].(string)
			if id == "" {
				writeResp(&ipc.Response{Error: "missing id parameter"})
				return
			}
			ar, ok := approvals.Get(id)
			if !ok {
				writeResp(&ipc.Response{Error: "approval " + id + " not found"})
				return
			}
			writeResp(&ipc.Response{Data: ar})

		case ipc.MethodApprovalDecide:
			id, _ := req.Params["id"].(string)
			optRaw := req.Params["option"]
			optNum := 0
			switch v := optRaw.(type) {
			case float64:
				optNum = int(v)
			case int:
				optNum = v
			}
			if id == "" || optNum == 0 {
				writeResp(&ipc.Response{Error: "missing id or option parameter"})
				return
			}
			if err := approvals.Decide(id, optNum); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: fmt.Sprintf("decision recorded for %s", id)})

		case ipc.MethodRequest:
			prompt, _ := req.Params["prompt"].(string)
			if prompt == "" {
				writeResp(&ipc.Response{Error: "missing prompt parameter"})
				return
			}
			// Dispatch a clara.user.prompt CloudEvent to the event bus.
			sup.EmitPromptEvent(prompt)
			writeResp(&ipc.Response{Message: "request dispatched to evaluator"})

		case ipc.MethodActuatorList:
			list := sup.ActuatorInfos()
			writeResp(&ipc.Response{Data: list})

		case ipc.MethodActuatorRun:
			id, _ := req.Params["id"].(string)
			if id == "" {
				writeResp(&ipc.Response{Error: "missing id parameter"})
				return
			}
			if err := sup.RunActuator(ctx, id, req.Params["payload"]); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "actuator " + id + " dispatched"})

		default:
			writeResp(&ipc.Response{Error: "unknown method: " + req.Method})
		}
	}
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
	return zerolog.New(writer).Level(level).With().Timestamp().Logger()
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

// listEventTools returns serialized event tool entries for the given namespace.
func listEventTools(
	ctx context.Context,
	reg *registry.Registry,
	namespace string,
) []map[string]any {
	// Direct server owns its own clara_list_events.
	directTool := namespace + ".clara_list_events"
	if _, ok := reg.Tool(directTool); ok {
		return buildEventTools(ctx, reg, directTool, namespace)
	}

	return nil
}

// buildEventTools calls listTool and converts the results to event entries.
// targetNS is the namespace prefix to use in the returned tool names.
func buildEventTools(
	ctx context.Context,
	reg *registry.Registry,
	listTool string,
	targetNS string,
) []map[string]any {
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	res, err := reg.Call(callCtx, listTool, map[string]any{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "buildEventTools %s error: %v\n", listTool, err)
		return nil
	}

	var events []any

	if _, isList := res.([]any); !isList {
		if _, isMap := res.(map[string]any); !isMap {
			if b, err := json.Marshal(res); err == nil {
				var normalized any
				if err := json.Unmarshal(b, &normalized); err == nil {
					res = normalized
				}
			}
		}
	}

	switch v := res.(type) {
	case []any:
		events = v
	case map[string]any:
		if sc, ok := v["structuredContent"].([]any); ok {
			events = sc
		} else if content, ok := v["content"].([]any); ok && len(content) > 0 {
			if m, ok := content[0].(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					var parsed []any
					if err := json.Unmarshal([]byte(text), &parsed); err == nil {
						events = parsed
					} else {
						fmt.Fprintf(os.Stderr, "buildEventTools %s unmarshal error: %v text=%q\n", listTool, err, text)
					}
				}
			}
		}
	default:
		fmt.Fprintf(os.Stderr, "buildEventTools %s unexpected type: %T res=%v\n", listTool, res, res)
	}
	if len(events) == 0 {
		fmt.Fprintf(os.Stderr, "buildEventTools %s events empty\n", listTool)
		return nil
	}

	result := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		m, ok := ev.(map[string]any)
		if !ok {
			continue
		}
		rawName, _ := m["name"].(string)
		if rawName == "" {
			continue
		}

		displayName := targetNS + "." + rawName

		entry := map[string]any{
			"name":        displayName,
			"description": m["description"],
			"is_event":    true,
		}
		if params := parseEventParams(m["params"]); len(params) > 0 {
			entry["parameters"] = params
		}
		result = append(result, entry)
	}
	return result
}

// parseEventParams converts the heterogeneous params field from clara_list_events
// into a canonical list of parameter maps. Two shapes are accepted:
//
//   - []any of strings         — param names only (legacy)
//   - []any of map[string]any  — structured {name, type, description}
//   - map[string]any           — {paramName: typeDescription} dict (Swift bridge)
func parseEventParams(raw any) []map[string]any {
	switch v := raw.(type) {
	case []any:
		params := make([]map[string]any, 0, len(v))
		for _, item := range v {
			switch p := item.(type) {
			case string:
				params = append(params, map[string]any{"name": p, "type": "any"})
			case map[string]any:
				entry := map[string]any{"name": p["name"]}
				if t, ok := p["type"].(string); ok && t != "" {
					entry["type"] = t
				}
				if d, ok := p["description"].(string); ok && d != "" {
					entry["description"] = d
				}
				params = append(params, entry)
			}
		}
		return params
	case map[string]any:
		// Swift bridge format: {"param_name": "type or description"}
		params := make([]map[string]any, 0, len(v))
		for name, typ := range v {
			params = append(params, map[string]any{
				"name":        name,
				"description": fmt.Sprintf("%v", typ),
			})
		}
		sort.Slice(params, func(i, j int) bool {
			ni, _ := params[i]["name"].(string)
			nj, _ := params[j]["name"].(string)
			return ni < nj
		})
		return params
	}
	return nil
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

// isTerminalFile reports whether f is connected to an interactive terminal.
func isTerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func saveConfig(cfg *config.Config, log zerolog.Logger) error {
	path := cfgFile
	if path == "" {
		path = config.DefaultConfigPath()
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	log.Info().Str("path", path).Msg("configuration saved")
	return nil
}

// buildMCPServer constructs an MCPServer from a config entry.
func buildMCPServer(srv config.MCPServerConfig, log zerolog.Logger) *registry.MCPServer {
	if srv.IsHTTPServer() {
		return registry.NewHTTPMCPServer(
			srv.Name,
			srv.Description,
			srv.URL,
			srv.Token,
			srv.SkipVerify,
			log,
		)
	}
	args, err := srv.CommandArgs()
	if err != nil || len(args) == 0 {
		// Return a server that will fail to start with a clear error.
		return registry.NewMCPServer(
			srv.Name,
			srv.Description,
			srv.Command,
			nil,
			srv.ResolvedEnv(),
			cfg.MCPCommandSearchPathList(),
			log,
		)
	}
	return registry.NewMCPServer(
		srv.Name,
		srv.Description,
		args[0],
		args[1:],
		srv.ResolvedEnv(),
		cfg.MCPCommandSearchPathList(),
		log,
	)
}
