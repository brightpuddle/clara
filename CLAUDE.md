# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Clara is a terminal-centric "HUD" application that aggregates information from multiple sources (reminders, files, notes, email), prioritizes what needs attention using AI/embeddings, and automates data organization. It consists of three Go components plus a Swift native worker.

## Common Commands

```bash
# Install required toolchain (buf, protoc-gen-go, protoc-gen-go-grpc, goreman)
# Also install watchexec for file-watching: brew install watchexec
make setup

# Generate gRPC/protobuf Go stubs from .proto files
make proto

# Compile all Go binaries
make build

# Development workflow (recommended)
# Terminal 1: start all services with auto-rebuild on .go/.proto changes
make watch        # requires: brew install watchexec

# Terminal 2: run the TUI (leave it running; auto-reconnects to agent)
make dev-tui

# Start services once (no file watching)
make dev

# Auto-rebuild native worker on Swift changes (in a separate terminal)
make watch-native   # requires: brew install watchexec

# Build Swift native worker
make swift-build         # Release build
make swift-build-debug   # Debug build

# Open GUI app in Xcode for development
make gui-open

# Clean and tidy
make clean
make tidy
```

## Development Workflow

**Normal Go dev loop:**
1. `make watch` in Terminal 1 — starts server+agent+native; auto-kills/restarts on any `.go` or `.proto` change
2. `make dev-tui` in Terminal 2 — start the TUI; leave it running (reconnects automatically when agent restarts)

**Proto changes:**
- Edit `.proto` files; `make watch` detects the change, runs `buf generate`, restarts all services

**Swift changes:**
- Run `make watch-native` in a separate terminal; it rebuilds the native worker binary on Swift source changes
- The main `make watch` will restart the native worker process automatically when its binary is updated

**Full restart needed when:**
- Changing `go.mod` / `go.sum` (run `make tidy` first)
- Changing config schema (run `make build` then restart)

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
│  ┌──────────────┐  ┌───────────────┐                          │
│  │ SQLite +     │  │ Ollama        │                          │
│  │ sqlite-vec   │  │ (embeddings)  │                          │
│  └──────────────┘  └───────────────┘                          │
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
```

### Component Responsibilities

- **Agent** (`agent/`): Local data access, file watching, background ingestion; owns the SQLite+sqlite-vec database; calls Ollama directly for embeddings
- **TUI** (`tui/`): Terminal interface using Bubble Tea - 3-pane layout (artifacts/related/detail)
- **GUI** (`gui/`): Native macOS SwiftUI app - Apple Notes/Mail-style 3-column layout; connects to same agent socket
- **Native** (`native/`): macOS-specific: Reminders via EventKit, Spotlight search, system theme detection

### Key Internal Packages

- `internal/db/`: SQLite + sqlite-vec database operations (artifacts, embeddings, kNN search)
- `internal/embedding/`: Ollama HTTP client for generating embedding vectors
- `internal/artifact/`: Universal artifact model (unified representation of files, reminders, notes)
- `internal/config/`: Configuration loading from YAML + environment

### Communication Protocol

- **TUI/GUI ↔ Agent**: gRPC over Unix Domain Socket (`~/.local/share/clara/agent.sock`)
- **Agent ↔ Native Worker**: gRPC over Unix Domain Socket (`~/.local/share/clara/native.sock`)
- **Agent ↔ Ollama**: HTTP REST (`http://localhost:11434` by default)

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
