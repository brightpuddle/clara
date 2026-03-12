package main

import (
	"context"
	"os"
	"os/signal"
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
	reg.RegisterWithDesc("db.query", "Execute a read-only SQL SELECT against the Clara database.", db.QueryTool())
	reg.RegisterWithDesc("db.exec", "Execute a SQL write statement against the Clara database.", db.ExecTool())
	reg.RegisterWithDesc(
		"db.vec_search",
		"Perform a vector similarity search over embeddings in the Clara database.",
		db.VecSearchTool(),
	)

	for _, srv := range cfg.MCPServers {
		mcpSrv := registry.NewMCPServer(
			srv.Name, srv.Command, srv.Args, srv.ResolvedEnv(), logger,
		)
		reg.AddServer(mcpSrv)
	}

	it := interpreter.New(reg, logger).
		WithOnChange(func(ctx context.Context, runID, state string, mem map[string]any) {
			if err := db.SaveRunState(ctx, runID, "", state, mem); err != nil {
				logger.Warn().Err(err).Str("run_id", runID).Msg("failed to persist run state")
			}
		})

	sup := supervisor.New(cfg.TasksDir(), reg, it, logger)

	handler := buildHandler(reg, sup, logger)
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
					"active_intents": len(intents),
					"tools":          len(reg.Names()),
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
			tools := reg.Tools()
			result := make([]map[string]any, len(tools))
			for i, t := range tools {
				result[i] = map[string]any{
					"name":        t.Name,
					"description": t.Description,
				}
			}
			return &ipc.Response{Data: result}

		case ipc.MethodToolShow:
			name, _ := req.Params["name"].(string)
			if name == "" {
				return &ipc.Response{Error: "missing name parameter"}
			}
			return handleToolShow(reg, name)

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

// handleToolShow returns detailed capabilities for a server or a single tool.
// name can be a server name ("fs") or a qualified tool name ("fs.read_file").
func handleToolShow(reg *registry.Registry, name string) *ipc.Response {
	// Try exact server match first.
	if caps := reg.GetCapabilities(name); caps != nil {
		return &ipc.Response{Data: serializeCaps(caps)}
	}

	// Try matching as a specific tool (server.toolname).
	tools := reg.Tools()
	for _, t := range tools {
		if t.Name == name {
			return &ipc.Response{Data: map[string]any{
				"name":        t.Name,
				"description": t.Description,
			}}
		}
	}

	// Check if the name is a server prefix (e.g. querying "fs" when capabilities
	// weren't stored, but tools like "fs.read_file" exist — fallback).
	var matched []map[string]any
	prefix := name + "."
	for _, t := range tools {
		if len(t.Name) > len(prefix) && t.Name[:len(prefix)] == prefix {
			matched = append(matched, map[string]any{
				"name":        t.Name,
				"description": t.Description,
			})
		}
	}
	if len(matched) > 0 {
		return &ipc.Response{Data: map[string]any{
			"name":  name,
			"tools": matched,
		}}
	}

	return &ipc.Response{Error: "server or tool " + name + " not found"}
}

// serializeCaps converts a ServerCapabilities into a plain map for JSON transport.
func serializeCaps(caps *registry.ServerCapabilities) map[string]any {
	tools := make([]map[string]any, 0, len(caps.Tools))
	for _, t := range caps.Tools {
		params := extractParams(t)
		entry := map[string]any{
			"name":        t.Name,
			"description": t.Description,
		}
		if len(params) > 0 {
			entry["parameters"] = params
		}
		tools = append(tools, entry)
	}

	resources := make([]map[string]any, 0, len(caps.Resources))
	for _, r := range caps.Resources {
		resources = append(resources, map[string]any{
			"name":        r.Name,
			"uri":         r.URI,
			"description": r.Description,
			"mime_type":   r.MIMEType,
		})
	}

	prompts := make([]map[string]any, 0, len(caps.Prompts))
	for _, p := range caps.Prompts {
		prompts = append(prompts, map[string]any{
			"name":        p.Name,
			"description": p.Description,
		})
	}

	return map[string]any{
		"name":        caps.Name,
		"description": caps.Description,
		"tools":       tools,
		"resources":   resources,
		"prompts":     prompts,
	}
}

// extractParams pulls parameter info from a tool's input schema.
func extractParams(t mcp.Tool) []map[string]any {
	schema := t.InputSchema
	var params []map[string]any
	required := make(map[string]bool, len(schema.Required))
	for _, r := range schema.Required {
		required[r] = true
	}
	for name, prop := range schema.Properties {
		entry := map[string]any{"name": name, "required": required[name]}
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
	return params
}
