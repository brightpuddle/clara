# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Clara is a productivity HUD that aggregates information from multiple sources (reminders, files, notes), prioritizes what needs attention using AI/embeddings, and automates data organization. It consists of a Go agent with SQLite+Ollama, a Bubble Tea TUI, a Wails desktop app (Svelte+TypeScript), and a Swift native worker for macOS system integration.

## Common Commands

```bash
# Install required toolchain (buf, protoc-gen-go, protoc-gen-go-grpc, goreman, wails)
make setup

# Generate gRPC/protobuf Go stubs from .proto files
make proto

# Compile all Go binaries (agent + TUI)
make build

# Development workflow
make watch        # Auto-rebuild agent+native on .go/.proto changes (requires: brew install watchexec)
make dev-tui      # Run TUI (auto-reconnects when agent restarts)
make app-dev      # Run Clara desktop app in dev mode with hot reload (requires: wails)

# Build Swift native worker
make swift-build         # Release build
make swift-build-debug   # Debug build

# Build and run desktop app
make app-build    # Production build
make app-run      # Build and open Clara.app

# Clean and tidy
make clean
make tidy
make test         # Run all Go tests
```

## Development Workflow

**TUI dev loop:**
1. `make watch` in Terminal 1 — starts agent+native; auto-restarts on `.go`/`.proto` changes
2. `make dev-tui` in Terminal 2 — start the TUI; auto-reconnects when agent restarts

**Desktop app dev loop:**
1. `make watch` in Terminal 1 — runs the agent+native worker
2. `make app-dev` in Terminal 2 — runs Wails with hot reload (frontend changes reflect immediately)

**Proto changes:**
- Edit `.proto` files; `make watch` detects the change, runs `buf generate`, restarts services
- After proto changes, run `wails generate module` in `app/` to regenerate JS bindings

**Swift changes:**
- Run `make watch-native` in a separate terminal; it rebuilds the native worker binary on Swift source changes

## Architecture

```
┌─────────────────────────────────┐  ┌─────────────────────────────────┐
│        TUI (Bubble Tea)         │  │     Desktop App (Wails+Svelte)  │
│      bin/clara-tui (Go)         │  │         app/ (Go+TS)            │
└────────────────┬────────────────┘  └────────────────┬────────────────┘
                 │ gRPC over Unix socket               │ gRPC over Unix socket
                 └──────────────────┬─────────────────┘
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
                                 │ gRPC over Unix socket
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

- **Agent** (`agent/`): Local data access, file watching, background ingestion; owns the SQLite+sqlite-vec database; calls Ollama directly for embeddings
- **TUI** (`tui/`): Terminal interface using Bubble Tea - 3-pane layout (artifacts/related/detail)
- **Desktop App** (`app/`): Wails v2 desktop app with Svelte+TypeScript frontend; Apple Notes-style 3-column layout; connects to same agent socket as TUI
- **Native** (`native/`): macOS-specific: Reminders via EventKit, Spotlight search, system theme detection

### Desktop App Structure

```
app/
├── app.go              # Wails App struct — bound methods exposed to frontend
├── main.go             # Wails entry point — window/tray setup
├── agent/
│   └── client.go       # gRPC client to agent.sock
└── frontend/
    └── src/
        ├── App.svelte          # Root component, layout orchestration
        ├── lib/
        │   ├── agent.ts        # Typed wrappers around Wails Go bindings + event listeners
        │   └── store.ts        # Svelte stores: artifacts, selectedId, section, status
        └── components/
            ├── SidebarView.svelte
            ├── ArtifactListView.svelte
            ├── ArtifactRow.svelte
            ├── ArtifactDetailView.svelte
            └── SettingsModal.svelte
```

### Key Internal Packages

- `internal/db/`: SQLite + sqlite-vec database operations (artifacts, embeddings, kNN search)
- `internal/embedding/`: Ollama HTTP client for generating embedding vectors
- `internal/artifact/`: Universal artifact model (unified representation of files, reminders, notes)
- `internal/config/`: Configuration loading from YAML + environment

### Communication Protocol

- **TUI/Desktop App ↔ Agent**: gRPC over Unix Domain Socket (`~/.local/share/clara/agent.sock`)
- **Agent ↔ Native Worker**: gRPC over Unix Domain Socket (`~/.local/share/clara/native.sock`)
- **Agent ↔ Ollama**: HTTP REST (`http://localhost:11434` by default)
- **Wails Frontend ↔ Go Backend**: Wails bindings (`window.go.main.App.*`) + event bus (`runtime.EventsEmit`)

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

## Desktop App Keybindings

- `j`/`k`: Move up/down the artifact list
- `h`/`l`: Move focus between sidebar / list / detail
- `gg` / `G`: Jump to top / bottom of list
- `Space`: Toggle item done
- `Enter`: Open in `$EDITOR`
- `Cmd+,`: Open Settings

## Development Notes

- Protobuf definitions in `proto/` - regenerate with `make proto`
- Swift native worker uses grpc-swift - rebuild with `make swift-build`
- Agent supports `--debug` flag for verbose logging
- Log files written to `~/.local/share/clara/logs/clara.log` by default
- Wails app module lives at `app/` with its own `go.mod`; uses `replace github.com/brightpuddle/clara => ../` to import root module packages

