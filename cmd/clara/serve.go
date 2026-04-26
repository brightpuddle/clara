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

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/toolcatalog"
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
	if err := addMCPServers(reg, logger); err != nil {
		return err
	}

	registerPermanentTUITools(reg, db, logger)

	sup := supervisor.New(cfg.TasksDir(), reg, cfg.MCPStartupTimeout, func(
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
			initialRunState(intent),
			intent.WorkflowKind(),
			entrypoint,
			intent.Script,
			mem,
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

	loader := newPluginLoader(reg, sup, cfg, logger)
	if err := loader.loadAll(); err != nil {
		logger.Error().Err(err).Msg("failed to load native plugins")
	}

	handler := buildHandler(reg, sup, attachServer, db, logger, shutdown)
	controlServer, err := ipc.NewServer(cfg.ControlSocketPath(), handler, logger)
	if err != nil {
		return errors.Wrap(err, "create control socket server")
	}

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
					"servers":        reg.ActiveServerCount(),
					"intents":        len(intents),
					"active_intents": active,
					"tools":          len(reg.Names()),
					"dynamic_mcp":    len(reg.DynamicServerNames()),
				},
			})

		case ipc.MethodList:
			intents := sup.IntentInfos()
			type taskEntry struct {
				IntentID    string         `json:"intent_id"`
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
						Description: intent.Description,
						Error:       intent.Error,
					})
					continue
				}
				for _, task := range intent.Tasks {
					list = append(list, taskEntry{
						IntentID:    intent.ID,
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

			// Dispatch by task mode: on-demand fires a single run; auto tasks
			// (schedule/worker/event) activate the persistent loop.
			isOnDemand := intentTaskIsOnDemand(intent, taskName)
			if isOnDemand {
				runID := fmt.Sprintf("%s-manual-%d", intent.ID, time.Now().UnixNano())
				go runIntentInBackground(ctx, intent, runID, taskName, req.Args, reg, db, log)
				msg := "intent " + id + " started"
				if taskName != "" {
					msg = "intent " + id + " task " + taskName + " started"
				}
				writeResp(&ipc.Response{
					Message: msg,
					Data:    map[string]any{"run_id": runID},
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
					// Priority:
					// 1. Explicit namespace description (from config, built-in defaults, or native plugins)
					// 2. Server description from MCP capabilities
					if nsDesc := reg.NamespaceDescription(p.Name); nsDesc != "" {
						desc = nsDesc
					} else if caps := reg.GetCapabilities(p.Name); caps != nil && caps.Description != "" {
						desc = caps.Description
					}

					result = append(result, map[string]any{
						"name":        p.Name,
						"description": desc,
					})
				}
				writeResp(&ipc.Response{Data: result})
				return
			}
			// Filter out internal tools (clara_list_events is an implementation detail).
			allTools := filterTools(reg.Tools(), filter)
			visible := make([]registry.ToolInfo, 0, len(allTools))
			for _, t := range allTools {
				if !strings.HasSuffix(t.Name, ".clara_list_events") {
					visible = append(visible, t)
				}
			}
			result := make([]map[string]any, len(visible))
			for i, tool := range visible {
				result[i] = serializeToolInfo(tool)
			}
			// Collect unique server names from the visible tools and append
			// their event tools so event streams appear alongside regular tools.
			seenServers := make(map[string]bool)
			for _, t := range visible {
				if parts := strings.SplitN(t.Name, ".", 2); len(parts) == 2 {
					seenServers[parts[0]] = true
				}
			}
			for serverName := range seenServers {
				result = append(result, listEventTools(ctx, reg, serverName)...)
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

			args := map[string]any{}
			if rawArgs, ok := req.Params["args"]; ok && rawArgs != nil {
				parsedArgs, ok := rawArgs.(map[string]any)
				if !ok {
					writeResp(&ipc.Response{Error: "args parameter must be an object"})
					return
				}
				args = parsedArgs
			}

			result, err := reg.Call(ctx, name, args)
			if err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Data: result})

		case ipc.MethodMCPRegister:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			registration, err := attach.Register(name)
			if err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{
				Message: "dynamic MCP registration created",
				Data:    registration,
			})

		case ipc.MethodMCPUnregister:
			name, _ := req.Params["name"].(string)
			if name == "" {
				writeResp(&ipc.Response{Error: "missing name parameter"})
				return
			}
			if err := attach.Unregister(name); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "dynamic MCP registration removed"})

		case ipc.MethodMCPList:
			statuses := reg.ServerStatuses()
			managed := make([]map[string]any, 0, len(statuses))
			for name, status := range statuses {
				managed = append(managed, map[string]any{
					"name":   name,
					"status": string(status),
				})
			}
			sort.Slice(managed, func(i, j int) bool {
				return managed[i]["name"].(string) < managed[j]["name"].(string)
			})
			writeResp(&ipc.Response{Data: map[string]any{
				"managed": managed,
				"active":  reg.DynamicServerNames(),
				"pending": attach.Registrations(),
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

			// Check if already exists in config
			var existingIdx = -1
			for i, srv := range cfg.MCPServers {
				if srv.Name == name {
					existingIdx = i
					break
				}
			}

			if existingIdx != -1 && !overwrite {
				writeResp(&ipc.Response{Error: "server " + name + " already exists; use overwrite:true to update"})
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

			// Add to config
			if existingIdx != -1 {
				cfg.MCPServers[existingIdx] = newSrvCfg
			} else {
				cfg.MCPServers = append(cfg.MCPServers, newSrvCfg)
			}

			if err := saveConfig(cfg, log); err != nil {
				writeResp(&ipc.Response{Error: "failed to save config: " + err.Error()})
				return
			}

			// Update registry
			if existingIdx != -1 || reg.HasServer(name) {
				_ = reg.RemoveServer(name)
			}

			var mcpSrv *registry.MCPServer
			if newSrvCfg.IsHTTPServer() {
				mcpSrv = registry.NewHTTPMCPServer(
					newSrvCfg.Name,
					newSrvCfg.Description,
					newSrvCfg.URL,
					newSrvCfg.Token,
					newSrvCfg.SkipVerify,
					log,
				)
			} else {
				args, err := newSrvCfg.CommandArgs()
				if err != nil {
					writeResp(&ipc.Response{Error: err.Error()})
					return
				}
				if len(args) == 0 {
					writeResp(&ipc.Response{Error: "empty command"})
					return
				}
				mcpSrv = registry.NewMCPServer(
					newSrvCfg.Name,
					newSrvCfg.Description,
					args[0],
					args[1:],
					newSrvCfg.ResolvedEnv(),
					cfg.MCPCommandSearchPathList(),
					log,
				)
			}

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

		case ipc.MethodTUIHistory:
			limit, _ := req.Params["limit"].(float64)
			history, err := db.LoadTUIContentHistory(ctx, int(limit))
			if err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Data: history})

		case ipc.MethodTUIClear:
			if err := db.ClearTUIContentHistory(ctx); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "TUI history cleared"})

		case ipc.MethodTUIAnswer:
			id, _ := req.Params["id"].(float64)
			answer, _ := req.Params["answer"].(string)
			if id == 0 {
				writeResp(&ipc.Response{Error: "missing id"})
				return
			}
			if err := db.UpdateTUIContentAnswer(ctx, int64(id), answer); err != nil {
				writeResp(&ipc.Response{Error: err.Error()})
				return
			}
			writeResp(&ipc.Response{Message: "answer recorded"})

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

// listEventTools returns serialized event tool entries for the given namespace.
//
// Two cases are handled:
//
//  1. Direct server: "namespace.clara_list_events" exists (e.g. "fs"). All
//     events are returned as "namespace.<eventName>", omitting any whose raw
//     name is claimed by a sub-namespace prefix.
//
//  2. Sub-namespace: namespace is registered as a child of a parent server
//     (e.g. "reminders" → server "macos", prefix "reminders"). Only events
//     whose raw name starts with "<prefix>_" are included, and they are
//     presented as "namespace.<suffix>" (e.g. "reminders.on_change").
func listEventTools(ctx context.Context, reg *registry.Registry, namespace string) []map[string]any {
	// Case 1: direct server owns its own clara_list_events.
	directTool := namespace + ".clara_list_events"
	if _, ok := reg.Tool(directTool); ok {
		subNSPrefixes := reg.ServerNamespacePrefixes(namespace)
		return buildEventTools(ctx, reg, directTool, namespace, "", subNSPrefixes)
	}

	// Case 2: namespace is a sub-namespace mapped from a parent server.
	parentServer, prefix, ok := reg.NamespaceMeta(namespace)
	if !ok {
		return nil
	}
	parentTool := parentServer + ".clara_list_events"
	if _, ok := reg.Tool(parentTool); !ok {
		return nil
	}
	return buildEventTools(ctx, reg, parentTool, namespace, prefix, nil)
}

// buildEventTools calls listTool and converts the results to event entries.
// targetNS is the namespace prefix to use in the returned tool names.
// prefixFilter, when non-empty, restricts results to events whose raw name
// starts with "<prefixFilter>_" and strips that prefix from the display name.
// subNSPrefixes maps sub-namespace → prefix and is used in the direct-server
// case to omit events already claimed by a finer-grained namespace.
func buildEventTools(
	ctx context.Context,
	reg *registry.Registry,
	listTool string,
	targetNS string,
	prefixFilter string,
	subNSPrefixes map[string]string,
) []map[string]any {
	res, err := reg.Call(ctx, listTool, nil)
	if err != nil {
		return nil
	}
	events, ok := res.([]any)
	if !ok {
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

		var displayName string
		if prefixFilter != "" {
			// Sub-namespace mode: include only events matching "<prefix>_".
			if !strings.HasPrefix(rawName, prefixFilter+"_") {
				continue
			}
			displayName = targetNS + "." + strings.TrimPrefix(rawName, prefixFilter+"_")
		} else {
			// Direct-server mode: skip events claimed by a sub-namespace.
			claimed := false
			for _, p := range subNSPrefixes {
				if strings.HasPrefix(rawName, p+"_") {
					claimed = true
					break
				}
			}
			if claimed {
				continue
			}
			displayName = targetNS + "." + rawName
		}

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

func registerPermanentTUITools(reg *registry.Registry, db *store.Store, logger zerolog.Logger) {
	reg.RegisterDefault("tui.notify.send", func(ctx context.Context, args map[string]any) (any, error) {
		message, _ := args["message"].(string)
		if message == "" {
			return nil, errors.New("message is required")
		}

		// Always persist to DB
		runID, _ := ctx.Value(orchestrator.ContextKeyRunID).(string)
		intentID, _ := ctx.Value(orchestrator.ContextKeyIntentID).(string)
		id, _ := db.SaveTUIContent(ctx, store.TUIContentItem{
			RunID:    runID,
			IntentID: intentID,
			Kind:     "notification",
			Text:     message,
		})

		// Check if TUI is connected (dynamic MCP)
		if reg.Has("tui.hud_send") {
			args["id"] = id
			args["run_id"] = runID
			args["intent_id"] = intentID
			return reg.Call(ctx, "tui.hud_send", args)
		}

		return "notification recorded (TUI offline)", nil
	})

	reg.RegisterDefault("tui.notify.send_interactive", func(ctx context.Context, args map[string]any) (any, error) {
		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return nil, errors.New("prompt is required")
		}
		var opts []string
		if raw, ok := args["options"].([]any); ok {
			for _, r := range raw {
				if s, ok := r.(string); ok {
					opts = append(opts, s)
				}
			}
		}

		runID, _ := ctx.Value(orchestrator.ContextKeyRunID).(string)
		intentID, _ := ctx.Value(orchestrator.ContextKeyIntentID).(string)

		// Check for existing answer in DB for this intent and prompt
		if intentID != "" {
			answer, _ := db.GetTUIAnswer(ctx, intentID, prompt)
			if answer != "" {
				// Record it in history as answered so TUI shows it
				_, _ = db.SaveTUIContent(ctx, store.TUIContentItem{
					RunID:    runID,
					IntentID: intentID,
					Kind:     "qa",
					Text:     prompt,
					Options:  opts,
					Answer:   answer,
				})
				return fmt.Sprintf("Answer received: %s", answer), nil
			}
		}

		// Always persist to DB (or reuse existing unanswered prompt)
		var id int64
		if intentID != "" {
			id, _ = db.GetUnansweredTUIPrompt(ctx, intentID, prompt)
		}
		
		if id == 0 {
			id, _ = db.SaveTUIContent(ctx, store.TUIContentItem{
				RunID:    runID,
				IntentID: intentID,
				Kind:     "qa",
				Text:     prompt,
				Options:  opts,
			})
		}

		// If TUI is connected, proxy immediately
		if reg.Has("tui.hud_send_interactive") {
			args["id"] = id
			args["run_id"] = runID
			args["intent_id"] = intentID
			return reg.Call(ctx, "tui.hud_send_interactive", args)
		}

		// If no RunID is present, this is likely a direct CLI tool call.
		// In this case, we block and wait for the answer to be recorded in the DB.
		if runID == "" {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					// If CLI call is cancelled (e.g. Ctrl+C), remove the prompt from DB
					// so it doesn't appear in TUI later.
					_ = db.DeleteTUIContent(context.Background(), id)
					return nil, ctx.Err()
				case <-ticker.C:
					// Check if answered
					history, _ := db.LoadTUIContentHistory(ctx, 100)
					for _, item := range history {
						if item.ID == id && item.Answer != "" {
							return fmt.Sprintf("Answer received: %s", item.Answer), nil
						}
					}
				}
			}
		}

		// If TUI is offline and it's a script run, pause execution.
		return nil, errors.New("workflow paused: waiting for TUI input")
	})
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
