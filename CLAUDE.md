# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Clara is a local-first agentic orchestrator for macOS written in Go. It runs as a daemon that:
- Manages MCP (Model Context Protocol) server lifecycle
- Executes Starlark (`.star`) intent files
- Persists run history and supports wait/resume workflows
- Provides a CLI and TUI for daemon control

The core architectural principle: **If a capability is available to intents, it must be delivered through MCP.**

## Common Commands

```bash
# Build the binary
go build -o clara ./cmd/clara

# Run tests
go test ./... -timeout 60s

# Run a single test
go test -v ./internal/config -run TestConfigName

# Format code (required before commit)
golines -m 100 --base-formatter goimports -w ./...
goimports -w ./...

# Lint
go vet ./...
staticcheck ./...

# Build the Swift macOS bridge
make bridge
```

## Key Dependencies

- **Go 1.26+** for daemon/CLI, **Swift 6.0+** for ClaraBridge
- `go.starlark.net` - Starlark intent execution
- `github.com/spf13/cobra` - CLI framework
- `github.com/mark3labs/mcp-go` - MCP client/server
- `github.com/ncruces/go-sqlite3` - Pure Go SQLite (no CGO)
- `github.com/rs/zerolog` - Structured logging
- `github.com/cockroachdb/errors` - Error wrapping with stacktraces

## Architecture

### Core Packages

| Package | Purpose |
|---------|---------|
| `cmd/clara/` | Unified binary: daemon, CLI, built-in MCP launchers |
| `internal/config/` | Config loading from YAML + `${ENV}` expansion |
| `internal/orchestrator/` | Intent model, compilation from `.star` files |
| `internal/registry/` | MCP server connections and unified tool registry |
| `internal/interpreter/` | Starlark execution loop with `tool()` and `wait()` builtins |
| `internal/supervisor/` | Tasks directory watcher, runtime mode lifecycle |
| `internal/store/` | SQLite persistence for runs, events, wait state |
| `internal/mcpserver/` | Built-in MCP servers (fs, db, ollama-embeddings, taskwarrior) |
| `internal/ipc/` | Control socket protocol for CLI↔daemon communication |
| `internal/tui/` | Interactive terminal UI with slash commands |
| `swift/` | Native macOS bridge (ClaraBridge) MCP server |

### Intent Execution Flow

1. `.star` file is placed in watched tasks directory (`~/.local/share/clara/tasks/`)
2. Supervisor detects and compiles it via `orchestrator.IntentLoader`
3. Runtime mode (on_demand/schedule/worker/event) determines execution policy
4. `interpreter.Execute()` runs Starlark with builtins: `tool()`, `wait()`
5. Results persisted to internal SQLite at `~/.local/share/clara/clara.db`

### Runtime Modes

- `on_demand`: Manual trigger via `clara intent trigger <id>`
- `schedule`: Cron-based execution
- `worker`: Immediate start + interval repeats
- `event`: Starts once, waits for external input via `wait()` or blocking MCP tools

## Configuration

- Config path: `~/.config/clara/config.yaml`
- Default data dir: `~/.local/share/clara/`
- Key paths derived from data_dir: `clara.db`, `clara.sock`, `tasks/`

## Error Handling

- Always wrap errors with `github.com/cockroachdb/errors.Wrap(err, "context")`
- Use `errors.Is` / `errors.As` for error inspection
- Never silently swallow errors

## Logging

- Use `zerolog` for structured logging
- Include contextual fields (`intent_id`, `state`, `tool`) in log events
- MCP servers must use stderr for diagnostics (stdout reserved for protocol)