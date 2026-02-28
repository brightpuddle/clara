# Clara

A personal information management system and AI assistant built on your own data.

**Phase 1 MVP**: Markdown backlink suggestions — semantic analysis of your notes to surface missing `[[wikilinks]]`, reviewed and approved through a TUI.

## Architecture

```
M4 Mac Mini
┌─────────────────────────────────────────────────────┐
│ brew services (native, full Metal GPU):             │
│   ollama        :11434  ← 5-10× faster than Docker │
│   clara-server  :8080, :50051                       │
│                                                     │
│ docker compose (infrastructure only):               │
│   postgres + pgvector  :5432                        │
│   temporal             :7233                        │
│   temporal-ui          :8088                        │
└─────────────────────────────────────────────────────┘
         │ gRPC :50051          │ REST :8080
         ▼                      ▼
  clara-agent (laptop)    clara TUI (anywhere on LAN)
  watches ~/notes/
```

> **Why native Ollama?** Docker/Podman on macOS runs inside a Linux VM with no
> access to Apple Silicon's Metal GPU. Running Ollama in a container gives
> CPU-only inference — 5–10× slower. Always run Ollama natively on macOS.

## Quickstart

### 1. Install native Ollama (M4 Mini, one-time)

```bash
make setup-ollama
```

This installs Ollama via Homebrew, starts it as a background service, and pulls
the `nomic-embed-text` embedding model.

### 2. Start infrastructure (postgres + temporal)

```bash
make docker-up
```

### 3. Build and start the server

```bash
make build-server
./clara-server
```

### 4. Start the agent (on your laptop)

```bash
make build-agent
CLARA_NOTES_DIR=~/notes CLARA_SERVER_ADDR=mac-mini.local:50051 ./clara-agent
```

The agent will:
1. Do an initial scan of your notes directory and ingest all `.md` files
2. Watch for subsequent changes and ingest them incrementally

### 5. Review suggestions in the TUI

Once the agent has ingested notes and the Temporal workflow has run link analysis:

```bash
CLARA_SERVER_URL=http://mac-mini.local:8080 ./clara
```

Keys:
- `y` — approve suggestion (agent will add the `[[wikilink]]` to the source note)
- `n` — reject suggestion
- `r` — refresh list
- `↑/↓` — navigate
- `/` — filter/search
- `q` — quit

### 6. Approved links

Within 10 seconds of approving, the agent will add the link to the appropriate note file:

```markdown
## See Also

- [[related-note-title]]
```

---

## Configuration

All configuration is via environment variables:

### Server

| Variable | Default | Description |
|---|---|---|
| `CLARA_DB_DSN` | `postgres://clara:clara@localhost:5432/clara?sslmode=disable` | Postgres connection string |
| `CLARA_OLLAMA_URL` | `http://localhost:11434` | Ollama base URL |
| `CLARA_TEMPORAL_HOST` | `localhost:7233` | Temporal gRPC address |
| `CLARA_GRPC_ADDR` | `:50051` | gRPC listen address |
| `CLARA_HTTP_ADDR` | `:8080` | REST API listen address |

### Agent

| Variable | Default | Description |
|---|---|---|
| `CLARA_SERVER_ADDR` | `localhost:50051` | clara-server gRPC address |
| `CLARA_NOTES_DIR` | `~/notes` | Root directory to watch |

### Client (TUI)

| Variable | Default | Description |
|---|---|---|
| `CLARA_SERVER_URL` | `http://localhost:8080` | clara-server HTTP base URL |

---

## REST API

The server exposes a versioned REST API at `/api/v1`:

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/suggestions?status=pending` | List suggestions |
| `POST` | `/api/v1/suggestions/{id}/approve` | Approve a suggestion |
| `POST` | `/api/v1/suggestions/{id}/reject` | Reject a suggestion |
| `GET` | `/api/v1/health` | Health check |

---

## Development

```bash
# Regenerate protobuf code (requires buf)
make proto

# Build all binaries
make build

# Run server locally (requires docker-compose stack running)
go run ./server

# Run agent (against local server)
CLARA_NOTES_DIR=~/notes go run ./agent

# Run TUI
go run ./client
```

### Temporal UI

Browse workflows at http://localhost:8088

---

## Monorepo Structure

```
clara/
├── proto/           # protobuf definitions (agent↔server gRPC)
├── pb/              # generated Go code (committed)
├── server/          # clara-server: API, RAG pipeline, Temporal workers
│   ├── api/         # REST handlers (chi)
│   ├── db/          # postgres schema + query layer (pgx/v5)
│   ├── grpc/        # gRPC ingest handler
│   ├── rag/         # text chunker + Ollama embedder
│   └── workers/     # Temporal workflow + activities
├── agent/           # laptop daemon
│   ├── actions/     # applies approved backlinks to .md files
│   ├── ingest/      # gRPC client
│   └── watcher/     # FSNotify markdown watcher
├── client/          # TUI (bubbletea)
│   └── tui/         # model, views, API client
└── docker/          # docker-compose, Dockerfile, configs
```

## Roadmap

- **Phase 2**: iOS/macOS SwiftUI clients, Tailscale for remote access
- **Phase 3**: Task sync (Apple Reminders ↔ TaskWarrior), email ingestion
- **Future**: Web UI (Templ/HTMX), content creation, cross-source insights
