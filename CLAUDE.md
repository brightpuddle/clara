# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Clara is a terminal-centric "HUD" application that aggregates information from multiple sources (reminders, files, notes, email), prioritizes what needs attention using AI/embeddings, and automates data organization. It consists of three Go components plus a Swift native worker.

## Common Commands

```bash
# Install required toolchain (buf, protoc-gen-go, protoc-gen-go-grpc, air, goreman)
make setup

# Generate gRPC/protobuf Go stubs from .proto files
make proto

# Compile all Go binaries
make build

# Run server, agent, and native worker together via goreman
make dev

# Run individual components with hot-reload
make dev-server   # Server with air (--debug enabled)
make dev-agent    # Agent with air (--debug enabled)
make dev-tui      # TUI directly (go run)

# Build Swift native worker
make swift-build         # Release build
make swift-build-debug   # Debug build

# Clean and tidy
make clean
make tidy
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         TUI (Bubble Tea)                        │
│                    bin/clara-tui (Go)                           │
└─────────────────────────────┬───────────────────────────────────┘
                              │ gRPC over Unix socket
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                          Agent (Go)                             │
│                    bin/clara-agent (Go)                         │
│  ┌──────────────┐  ┌───────────────┐  ┌────────────────────┐  │
│  │ File Watcher │  │ Reminders     │  │ Ingestor           │  │
│  │ (fsnotify)   │  │ (via native)  │  │ (background queue) │  │
│  └──────────────┘  └───────────────┘  └────────────────────┘  │
└─────────────────────────────┬───────────────────────────────────┘
                              │ gRPC over Unix socket
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Native Worker (Swift)                      │
│            native/.build/debug/NativeWorker                    │
│  ┌────────────────┐  ┌────────────────┐  ┌─────────────────┐  │
│  │ EventKit       │  │ Spotlight      │  │ Theme Detection │  │
│  │ (Reminders)   │  │ (mdfind)       │  │ (NSAppearance)  │  │
│  └────────────────┘  └────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘

      │ gRPC (TCP, can be remote)
      ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Server (Go)                              │
│                  bin/clara-server (Go)                          │
│  ┌──────────────┐  ┌───────────────┐  ┌────────────────────┐  │
│  │ SQLite +      │  │ Ollama        │  │ gRPC Server        │  │
│  │ sqlite-vec    │  │ (embeddings)  │  │                    │  │
│  └──────────────┘  └───────────────┘  └────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

- **Server** (`server/`): Vector embeddings, similarity search, LLM integration via Ollama
- **Agent** (`agent/`): Local data access, file watching, background ingestion, connects to server for AI
- **TUI** (`tui/`): Primary interface using Bubble Tea - master-detail layout with artifact list and related context panes
- **Native** (`native/`): macOS-specific: Reminders via EventKit, Spotlight search, system theme detection

### Key Internal Packages

- `internal/db/`: SQLite + sqlite-vec database operations
- `internal/embedding/`: Ollama client for embeddings
- `internal/artifact/`: Universal artifact model (unified representation of files, reminders, notes)
- `internal/config/`: Configuration loading from YAML + environment

### Communication Protocol

- **TUI ↔ Agent**: gRPC over Unix Domain Socket (`~/.local/share/clara/agent.sock`)
- **Agent ↔ Server**: gRPC over TCP (`localhost:50051`) - server can run remotely
- **Agent ↔ Native Worker**: gRPC over Unix Domain Socket (`~/.local/share/clara/native.sock`)

## Configuration

Configuration is loaded from (in order):
1. `~/.config/clara/config.yaml`
2. `~/.local/share/clara/config.yaml`
3. Current directory `./config.yaml`

Environment variables with `CLARA_` prefix override file values.

Key settings:
- `data_dir`: Database and socket location (default: `~/.local/share/clara`)
- `agent.watch_dirs`: Directories to monitor for file changes
- `ollama.url`: Ollama API endpoint (default: `http://localhost:11434`)
- `tui.theme_mode`: "dark", "light", or "system" (default: "system")

## TUI Keybindings

- `j`/`k`: Navigate within a list
- `h`/`l` or `Tab`/`Shift+Tab`: Cycle between Artifacts and Related Context panes
- `/`: Unified search across all artifacts
- `Space`: Toggle item as done/archived
- `Enter`: Drill down (open in `$EDITOR`)
- `o`: Open in native application
- `s`: Search within focused view

## Development Notes

- Protobuf definitions in `proto/` - regenerate with `make proto`
- Swift native worker uses grpc-swift - rebuild with `make swift-build`
- Server and Agent both support `--debug` flag for verbose logging
- Log files written to `~/.local/share/clara/logs/clara.log` by default
