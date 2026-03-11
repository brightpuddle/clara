// Package main is the entry point for the Clara daemon (clarad).
// It wires together the config, registry, interpreter, supervisor, bridge
// components into a single long-running process.
package main

import (
"context"
"os"
"os/signal"
"syscall"

"github.com/brightpuddle/clara/internal/config"
"github.com/brightpuddle/clara/internal/interpreter"
"github.com/brightpuddle/clara/internal/ipc"
"github.com/brightpuddle/clara/internal/orchestrator"
"github.com/brightpuddle/clara/internal/registry"
"github.com/brightpuddle/clara/internal/store"
"github.com/brightpuddle/clara/internal/supervisor"
"github.com/cockroachdb/errors"
"github.com/rs/zerolog"
"github.com/rs/zerolog/log"
"github.com/sourcegraph/conc"
)

func main() {
cfg, err := config.LoadDefault()
if err != nil {
log.Fatal().Err(err).Msg("failed to load config")
}

logger := buildLogger(cfg)

if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
logger.Fatal().Err(err).Str("path", cfg.DataDir).Msg("failed to create data dir")
}

logger.Info().
Str("data_dir", cfg.DataDir).
Str("log_level", cfg.LogLevelNormalized()).
Msg("clara daemon starting")

ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer cancel()

if err := run(ctx, cfg, logger); err != nil && !errors.Is(err, context.Canceled) {
logger.Fatal().Err(err).Msg("daemon error")
}
logger.Info().Msg("clara daemon stopped")
}

func run(ctx context.Context, cfg *config.Config, logger zerolog.Logger) error {
// Open the SQLite database.
db, err := store.Open(cfg.DBPath(), logger)
if err != nil {
return errors.Wrap(err, "open database")
}
defer db.Close()

// Build the tool registry.
reg := registry.New(logger)
reg.Register("db.query", db.QueryTool())
reg.Register("db.exec", db.ExecTool())
reg.Register("db.vec_search", db.VecSearchTool())

// Start configured MCP servers.
for _, srv := range cfg.MCPServers {
mcpSrv := registry.NewMCPServer(
srv.Name, srv.Command, srv.Args, srv.ResolvedEnv(), logger,
)
reg.AddServer(mcpSrv)
}

// Build the interpreter with crash-recovery persistence.
it := interpreter.New(reg, logger).
WithOnChange(func(ctx context.Context, runID, state string, mem map[string]any) {
// Persist run state so the daemon can resume after a restart.
// RunID format is "intent_id-timestamp", extract intent_id.
if err := db.SaveRunState(ctx, runID, "", state, mem); err != nil {
logger.Warn().Err(err).Str("run_id", runID).Msg("failed to persist run state")
}
})

// Build the supervisor.
sup := supervisor.New(cfg.TasksDir(), reg, it, logger)

// IPC control server handler.
handler := buildHandler(reg, sup, logger)
controlServer := ipc.NewServer(cfg.ControlSocketPath(), handler, logger)

wg := conc.NewWaitGroup()

// Start MCP servers.
wg.Go(func() {
if err := reg.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
logger.Error().Err(err).Msg("registry error")
}
})

// Start control server.
wg.Go(func() {
if err := controlServer.ListenAndServe(ctx); err != nil {
logger.Error().Err(err).Msg("control server error")
}
})

// Start supervisor.
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
// Signal the parent context — in practice this is done by os.Signal,
// but the CLI Stop command can also request it.
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
names := reg.Names()
tools := make([]map[string]any, len(names))
for i, name := range names {
tools[i] = map[string]any{"name": name}
}
return &ipc.Response{Data: tools}

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

func buildLogger(cfg *config.Config) zerolog.Logger {
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
