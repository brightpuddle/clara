# Clara Copilot Instructions

Clara is a productivity HUD that aggregates information from multiple sources (reminders, files, notes), prioritizes what needs attention using AI/embeddings, and automates data organization. It consists of a Go agent with SQLite+Ollama, a Bubble Tea TUI, a Wails desktop app (Svelte+TypeScript), and a Swift native worker for macOS system integration.

## Quick Start

### Setup
```bash
make setup  # Install: buf, protoc-gen-go, protoc-gen-go-grpc, goreman
            # (Also: brew install watchexec)
```

### Development Workflows

**TUI Dev Loop:**
```bash
# Terminal 1: Auto-rebuild on Go/proto changes
make watch

# Terminal 2: Run TUI; auto-reconnects when agent restarts
make dev-tui
```

**Desktop App Dev Loop:**
```bash
# Terminal 1: Run agent+native worker
make watch

# Terminal 2: Run Wails with hot reload
make app-dev
```

**Proto Changes Workflow:**
1. Edit `.proto` files in `proto/`
2. `make watch` detects changes, auto-runs `buf generate`
3. In `app/` directory, run `wails generate module` to regenerate JS bindings

### Build and Test

```bash
make proto        # Regenerate gRPC stubs from .proto files
make build        # Compile Go binaries (agent + TUI)
make test         # Run all tests

# Single test example:
go test ./internal/db -run TestUpsertAndGetArtifact -v

make swift-build-debug    # Build native worker (debug)
make swift-build          # Build native worker (release)
make app-build            # Production desktop app build
```

## Architecture

```
┌─────────────────────────────────┐  ┌─────────────────────────────────┐
│        TUI (Bubble Tea)         │  │     Desktop App (Wails+Svelte)  │
│      bin/clara-tui (Go)         │  │         app/ (Go+TS)            │
└────────────────┬────────────────┘  └────────────────┬────────────────┘
                 │ gRPC Unix socket               │ gRPC Unix socket
                 └──────────────────┬─────────────┘
                                    ▼
┌───────────────────────────────────────────────────────────────────────┐
│                            Agent (Go)                                 │
│                       bin/clara-agent (Go)                            │
│  ┌──────────────┐  ┌───────────────┐  ┌────────────────────────────┐ │
│  │ File Watcher │  │ Reminders     │  │ Ingestor (background queue)│ │
│  │ (fsnotify)   │  │ (via native)  │  └────────────────────────────┘ │
│  └──────────────┘  └───────────────┘                                 │
│  ┌──────────────┐  ┌───────────────┐                                 │
│  │ SQLite +     │  │ Ollama        │                                 │
│  │ sqlite-vec   │  │ (embeddings)  │                                 │
│  └──────────────┘  └───────────────┘                                 │
└────────────────────────────────┬──────────────────────────────────────┘
                                 │ gRPC Unix socket
                                 ▼
┌───────────────────────────────────────────────────────────────────────┐
│                       Native Worker (Swift)                           │
│                 native/.build/debug/NativeWorker                     │
│  ┌────────────────┐  ┌────────────────┐  ┌─────────────────────────┐ │
│  │ EventKit       │  │ Spotlight      │  │ Theme Detection         │ │
│  │ (Reminders)    │  │ (mdfind)       │  │ (NSAppearance)          │ │
│  └────────────────┘  └────────────────┘  └─────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

- **Agent** (`agent/`): SQLite+sqlite-vec database ownership; local file watching; background ingestion; Ollama direct calls
- **TUI** (`tui/`): Terminal interface (Bubble Tea); 3-pane layout (artifacts/related/detail)
- **Desktop App** (`app/`): Wails v2 with Svelte+TypeScript; Apple Notes-style 3-column layout; connects to same agent socket
- **Native Worker** (`native/`): macOS-specific (EventKit reminders, Spotlight search, system theme)

## Core Data Model

The **Artifact** is the universal data unit. Defined in `proto/artifact/v1/artifact.proto`:

```protobuf
enum ArtifactKind {
  ARTIFACT_KIND_FILE = 1;
  ARTIFACT_KIND_NOTE = 2;
  ARTIFACT_KIND_REMINDER = 3;
  ARTIFACT_KIND_EMAIL = 4;
  ARTIFACT_KIND_BOOKMARK = 5;
  ARTIFACT_KIND_LOG = 6;
  ARTIFACT_KIND_SUGGESTION = 7;
  ARTIFACT_KIND_TASK = 8;
}

message Artifact {
  string id;
  ArtifactKind kind;
  string title;
  string content;           // markdown or plain text
  string source_path;       // filesystem path, URL, or native ID
  string source_app;        // "reminders", "mail", "filesystem", etc.
  double heat_score;        // priority/urgency (0.0-1.0)
  bool done;
  repeated string tags;
  map<string, string> metadata;
  Timestamp created_at;
  Timestamp updated_at;
  Timestamp due_at;         // for reminders
}
```

## Communication Protocols

| Channel | Protocol | Location |
|---------|----------|----------|
| TUI/Desktop App ↔ Agent | gRPC over Unix socket | `~/.local/share/clara/agent.sock` |
| Agent ↔ Native Worker | gRPC over Unix socket | `~/.local/share/clara/native.sock` |
| Agent ↔ Ollama | HTTP REST | `http://localhost:11434` (default) |
| Wails Frontend ↔ Go Backend | Wails bindings + event bus | `window.go.main.App.*` + `runtime.EventsEmit` |

## Key Conventions

### Proto Organization and Code Generation

- Proto files live in `proto/{agent,artifact,native}/v1/` with corresponding Go output to `gen/`
- Generation is managed by `buf.yaml` and `buf.gen.yaml`:
  - Remote plugins: `buf.build/protocolbuffers/go` and `buf.build/grpc/go`
  - Output: `gen/` directory, `paths=source_relative` option
- Regenerate stubs: `make proto` (runs `buf generate`)
- Desktop app: After proto changes, run `cd app && wails generate module` to regenerate TypeScript bindings

### gRPC Communication

**Server Setup (Agent):**
- `agent/grpc/server.go`: `ListenUnix(socketPath)` creates Unix socket listener
- `agentv1.RegisterAgentServiceServer(grpcSrv, agentServer)` registers service
- Runs in background goroutine in `Agent.Run()`

**Client Setup (TUI/Desktop):**
- `tui/grpc/client.go`: Dials Unix socket with `grpc.Dial("unix:" + socketPath, ...)`
- Desktop app wraps in Go bindings exposed to Svelte frontend via `window.go.main.App.*`
- Both auto-reconnect on agent restart (connection is re-dialed)

**Native Worker Connection:**
- Agent calls `agentgrpc.DialNative(nativeSocketPath)` to get client
- Returns `nativev1.NativeWorkerServiceClient`
- Used to fetch reminders and mark them done (bidirectional)

### SQLite Schema and Database Usage

Located at `~/.local/share/clara/clara.db` (configurable).

**Key Tables:**
- `artifacts`: Core schema with TEXT id (PK), INTEGER kind, TEXT title/content, INTEGER heat_score DESC, etc.
  - JSON storage for tags (TEXT) and metadata (TEXT)
  - Timestamps as INTEGER unix epoch; due_at nullable
- `artifact_embeddings`: BLOB embedding (float32 little-endian), INTEGER dim
  - Only populated for searchable content
- `ops_log`: Reversibility log (for undo/replay)

**Connection Pattern:**
- `internal/db/db.go`: `Open(path)` → `*sql.DB` with `SetMaxOpenConns(1)` (single writer, multi-reader)
- Migrations run on `Open()` via `migrate()` idempotently
- Custom helpers: `UpsertArtifact()`, `ListArtifacts()`, `KNNSearch()`, `GetArtifact()`
- All public methods accept `ctx context.Context` for cancellation

**In-Memory Search:**
- `KNNSearch()` loads all embeddings into memory for cosine distance calculation
- Candidate prefilter limits to recent 1000 items minimum before calculating distances
- Note: Large datasets should migrate to sqlite-vec or external ANN library

### Embedding and Search

**Ollama Integration (`internal/embedding/`):**
- HTTP client calling `POST /api/embeddings` on Ollama server
- Default model: `nomic-embed-text` (768 dimensions)
- Returns `[]float32` vectors
- Used by ingestor to embed artifact content during ingestion

**Search (`proto/agent/v1/agent.proto`):**
- `Search(SearchRequest)` RPC performs hybrid search:
  - Text-based full-text search in artifact title/content
  - Vector-based kNN search on embeddings
  - Results sorted by relevance score
- Excludes `done=true` artifacts by default

### Patterns for Adding Features

1. **Define Data in Proto:**
   - Add fields to `Artifact` message in `proto/artifact/v1/artifact.proto`
   - Add service RPC if needed to `proto/agent/v1/agent.proto`
   - Run `make proto` to regenerate Go stubs

2. **Database Layer:**
   - Add schema migration in `internal/db/db.go` `migrate()` function
   - Add helper methods for new queries (e.g., `UpsertArtifact`, `SearchByField`)
   - Use context for cancellation

3. **Agent Service Layer:**
   - Implement RPC handler in `agent/grpc/server.go`
   - Use `AgentServer` receiver to access `db`, `nativeClient`, `taskwarriorWorker`, etc.
   - Broadcast events via `Broadcast(*agentv1.ArtifactEvent)` for subscribers

4. **Background Workers:**
   - Create package in `agent/{feature}/` with `New()` constructor and `Run(ctx context.Context)` method
   - Use `Ingestor` pattern: consume from event channel, write to database, push notifications
   - Example: `Ingestor`, `RemindersWorker`, `TaskwarriorWorker`, `FilesystemWatcher`

5. **TUI/Desktop UI:**
   - Add Bubble Tea message and update logic in `tui/` or Svelte component in `app/frontend/`
   - Call agent service via gRPC client

### Error Handling Conventions

- Use `github.com/cockroachdb/errors` for wrapping: `errors.Wrap(err, "context message")`
- Use `errors.Errorf(...)` for formatted errors
- All public functions return `error` as last return value
- Context errors propagate; no silent failures
- Log errors at appropriate level: `logger.Error().Err(err).Msg("description")`
- For optional integrations (native worker, reminders): log warnings if unavailable but don't crash

### Go Module Organization

- Root module: `github.com/brightpuddle/clara` (go 1.25.0, go.work)
- `go.work` uses workspaces to link root and `./app` sub-module
- Desktop app has own `go.mod`: `github.com/brightpuddle/clara/app`
  - Uses `replace github.com/brightpuddle/clara => ../` to import root packages
- Native worker is Swift (separate build: `make swift-build`)

## Configuration

Configuration sources (in order, later overrides earlier):
1. `~/.config/clara/config.yaml`
2. `~/.local/share/clara/config.yaml`
3. Current directory `./config.yaml`
4. Environment variables with `CLARA_` prefix

**Key Settings:**
- `data_dir`: Database and socket location (default: `~/.local/share/clara`)
- `log_file`: Path to log file (default: `~/.local/share/clara/logs/clara.log`)
- `log_level`: "debug", "info", "warn", "error" (default: "info")
- `native_worker_path`: Path to Swift binary (auto-detected if not set)
- `ollama.url`: Ollama API endpoint (default: `http://localhost:11434`)
- `ollama.embed_model`: Embedding model name (default: `nomic-embed-text`)
- `integrations.filesystem.enabled`: Watch filesystem (default: true)
- `integrations.filesystem.watch_dirs`: List of directories to monitor
- `integrations.reminders.enabled`: Sync reminders from EventKit (default: true, macOS only)
- `integrations.taskwarrior.enabled`: Sync taskwarrior tasks (default: false)
- `tui.theme_mode`: "dark", "light", or "system" (default: "system")

## Testing

**Run Tests:**
```bash
# All tests
make test

# Single package
go test ./internal/db -v

# Single test function
go test ./internal/db -run TestUpsertAndGetArtifact -v

# With coverage
go test ./... -cover

# With short timeout
go test -short ./...
```

**Test Patterns:**
- Tests live alongside source code (e.g., `artifacts_test.go` next to `artifacts.go`)
- Use table-driven tests for multiple cases
- Setup: `openTestDB(t *testing.T) *DB` opens in-memory SQLite
- Cleanup: `t.Cleanup(func() { db.Close() })`
- Use `testing.T` helpers: `t.Fatalf()`, `t.Errorf()`, `t.Helper()`

## Directory Structure

```
.
├── agent/                    # Go agent daemon
│   ├── agent.go             # Main Agent struct and Run() entry point
│   ├── grpc/                # gRPC server implementation
│   ├── ingestor/            # File ingestion worker
│   ├── reminders/           # Reminders sync worker
│   ├── taskwarrior/         # Taskwarrior sync worker
│   └── watcher/             # Filesystem watcher
├── cmd/
│   ├── agent/               # Agent binary entry point
│   └── tui/                 # TUI binary entry point
├── app/                     # Desktop app (Wails)
│   ├── app.go               # Wails App struct with bound methods
│   ├── main.go              # Window/tray setup
│   ├── agent/               # gRPC client to agent.sock
│   ├── frontend/            # Svelte+TypeScript UI
│   └── go.mod               # Sub-module
├── tui/                     # Terminal UI (Bubble Tea)
│   ├── model.go             # TUI state model
│   ├── panes/               # 3-pane components
│   └── grpc/                # gRPC client to agent.sock
├── native/                  # Swift native worker
│   └── Sources/             # Swift implementation
├── proto/                   # Protocol Buffer definitions
│   ├── agent/v1/            # Agent service proto
│   ├── artifact/v1/         # Artifact data model proto
│   └── native/v1/           # Native worker service proto
├── internal/                # Shared Go packages
│   ├── artifact/            # Heat score computation
│   ├── config/              # Configuration loading
│   ├── db/                  # SQLite operations
│   ├── embedding/           # Ollama HTTP client
│   ├── service/             # OS service integration
│   └── theme/               # UI theme management
├── gen/                     # Generated proto stubs (git-ignored)
├── bin/                     # Compiled binaries (git-ignored)
├── Makefile                 # Build targets
├── go.mod / go.work         # Module definition
├── buf.yaml / buf.gen.yaml  # Buf proto config
└── CLAUDE.md                # Extended developer notes
```

## Common Commands Reference

```bash
# Setup & Dependencies
make setup                   # Install toolchain (buf, protoc, goreman, etc.)

# Development
make watch                   # Auto-rebuild on .go/.proto changes + restart via goreman
make dev-tui                 # Run TUI (auto-reconnects to agent)
make app-dev                 # Run Wails with hot reload

# Build
make proto                   # Generate gRPC stubs
make build                   # Compile Go binaries
make swift-build-debug       # Build native worker (debug)
make swift-build             # Build native worker (release)
make app-build               # Build desktop app
make app-run                 # Build and open Clara.app

# Testing & Cleanup
make test                    # Run all Go tests
make clean                   # Remove binaries
make tidy                    # go mod tidy

# Development Commands (from Procfile via goreman)
make dev                     # Start agent + native via goreman
```

## Tips

- Use `--debug` flag with agent for verbose logging: `go run ./cmd/agent -- --debug`
- Agent socket: `~/.local/share/clara/agent.sock` (used by TUI/Desktop App)
- Native worker socket: `~/.local/share/clara/native.sock` (used by Agent)
- Logs: `~/.local/share/clara/logs/clara.log`
- Proto changes: Always run `make proto` after editing `.proto` files
- Desktop app proto: Also run `cd app && wails generate module` after proto changes
- Swift changes: Run `make watch-native` in separate terminal for auto-rebuild
- Check config: `$EDITOR ~/.config/clara/config.yaml`
