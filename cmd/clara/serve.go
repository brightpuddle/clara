package main

import (
	"context"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

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
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Clara agent in the foreground",
	Long: `Start the Clara background agent in the foreground.

The agent watches the tasks directory for Markdown intent files, converts them
via an LLM, and executes the resulting state machines. Use a process manager
(launchd, systemd, etc.) to run this as a persistent daemon.`,
	RunE:         runServe,
	SilenceUsage: true,
}

func runServe(cmd *cobra.Command, args []string) error {
	logger := buildLogger()

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return errors.Wrapf(err, "create data dir %q", cfg.DataDir)
	}

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

	it := interpreter.New(reg, logger).
		WithOnChange(func(ctx context.Context, runID, state string, mem map[string]any) {
			if err := db.SaveRunState(ctx, runID, "", state, mem); err != nil {
				logger.Warn().Err(err).Str("run_id", runID).Msg("failed to persist run state")
			}
		})

	sup := supervisor.New(cfg.TasksDir(), reg, it, logger)
	attachServer := registry.NewDynamicAttachServer(cfg.DynamicMCPSocketPath(), reg, logger)

	handler := buildHandler(reg, sup, attachServer, logger)
	controlServer := ipc.NewServer(cfg.ControlSocketPath(), handler, logger)

	wg := conc.NewWaitGroup()

	wg.Go(func() {
		if err := reg.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error().Err(err).Msg("registry error")
		}
	})

	wg.Go(func() {
		if err := controlServer.ListenAndServe(ctx); err != nil {
			logger.Error().Err(err).Msg("control server error")
		}
	})

	wg.Go(func() {
		if err := attachServer.ListenAndServe(ctx); err != nil {
			logger.Error().Err(err).Msg("dynamic MCP attach server error")
		}
	})

	wg.Go(func() {
		if err := sup.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
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
	log zerolog.Logger,
) ipc.HandlerFunc {
	return func(ctx context.Context, req *ipc.Request) *ipc.Response {
		switch req.Method {
		case ipc.MethodShutdown:
			return &ipc.Response{Message: "shutdown initiated"}

		case ipc.MethodStatus:
			intents := sup.ActiveIntents()
			return &ipc.Response{
				Message: "running",
				Data: map[string]any{
					"servers":        reg.ActiveServerCount(),
					"intents":        len(intents),
					"active_intents": len(intents),
					"tools":          len(reg.Names()),
					"dynamic_mcp":    len(reg.DynamicServerNames()),
				},
			}

		case ipc.MethodList:
			intents := sup.ActiveIntents()
			list := make([]map[string]any, len(intents))
			for i, intent := range intents {
				list[i] = map[string]any{
					"id":          intent.ID,
					"description": intent.Description,
				}
			}
			return &ipc.Response{Data: list}

		case ipc.MethodRun:
			id, _ := req.Params["id"].(string)
			if id == "" {
				return &ipc.Response{Error: "missing intent id"}
			}
			for _, intent := range sup.ActiveIntents() {
				if intent.ID == id {
					go runIntentInBackground(ctx, intent, reg, log)
					return &ipc.Response{Message: "intent " + id + " triggered"}
				}
			}
			return &ipc.Response{Error: "intent " + id + " not found"}

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
	reg *registry.Registry,
	log zerolog.Logger,
) {
	it := interpreter.New(reg, log)
	err := it.Execute(ctx, intent, intent.InitialState, interpreter.RunOptions{
		RunID: intent.ID + "-manual",
	})
	if err != nil {
		log.Error().Err(err).Str("intent_id", intent.ID).Msg("manual intent run error")
	}
}

func buildLogger() zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevelNormalized())
	if err != nil {
		level = zerolog.InfoLevel
	}
	if fi, _ := os.Stderr.Stat(); fi != nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			Level(level).
			With().Timestamp().Logger()
	}
	return zerolog.New(os.Stdout).Level(level).With().Timestamp().Logger()
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
