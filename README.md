# Clara

> "Reliably, consistently, and efficiently automate everything that can be
> automated."

Clara is an efficient, reliable, and resource-agnostic agentic orchestrator,
designed to reduce the cognitive and emotional load of modern digital life. It
is a personal assistant, "Data Janitor" and central HUD—a system built to handle
the repetitive, the messy, and the mundane so we can focus on what actually
matters, whether on macOS or Linux.

## The Vision

Clara is intended to automate all parts of our digital life, to help focus on
the things that really matter.

## The Clara Philosophy: Resource-Agnostic, Reliable Orchestration

The current AI ecosystem often defaults to an "AI-first" workflow where a model
is the central engine. Clara takes a different stance:

**AI is a tool, not the interpreter.**

We prioritize reliability and efficiency by treating AI as a component within a
deterministic framework. Think of Clara like CI/CD for your life: AI might
help write the actions or perform a specific step within them, but the execution
itself is a reliable, repeatable, and inspectable workflow that runs anywhere.

### Why Deterministic Intents?

- **Reliability:** You shouldn't have to wonder if your file organizer "felt
  like" working today.
- **Efficiency:** Running a native binary is orders of magnitude faster and
  cheaper than prompting an LLM for every step.
- **Inspectability:** You can diff, version, and improve your rulesets over
  time using standard software engineering tools.
- **Durable State:** Clara manages long-running tasks that can wait for human
  input or external events without keeping a "hot" LLM context alive.

## Architecture at a Glance

Clara is built on three core pillars:

1. **The Registry (MCP-First):** Clara is an aggregator for
   [Model Context Protocol](https://modelcontextprotocol.io) servers. If a
   capability exists (Filesystem, Chrome, Slack, Photos), it is delivered
   through an MCP server. This keeps the core daemon focused on orchestration
   and policy.
2. **The Intents (Native Go):** High-level workflows are authored as Native Go
   plugins. These are compiled binaries that define how tools should be used,
   when they should trigger, and how they should handle state, communicating
   with Clara via a high-performance RPC interface.
3. **The Daemon:** A Go-based background service that manages MCP server
   lifecycles, discovers native plugins, executes intents, and persists
   state in a local SQLite store.

## Getting Started

### Installation

For a streamlined installation on macOS:

```bash
curl -s https://raw.githubusercontent.com/brightpuddle/clara/main/scripts/install.sh | bash
```

This installs the `clara` binary, the `ClaraBridge` (for native macOS
integrations like Photos/Reminders), and the companion Chrome extension.

### Your First Intent

Native intents are authored in Go and compiled into plugins. Once installed in `~/.config/clara/intents/`, they are automatically discovered by the daemon.

Run an intent immediately:

```bash
clara intent start hello
clara intent logs hello
```

## Writing Intents

Intents are more than just scripts; they are managed tasks. Clara supports
several execution modes:

- **On-Demand:** Triggered manually via CLI or TUI.
- **Scheduled:** Cron-style execution (e.g., `0 7 * * *` for your morning
  brief).
- **Worker:** Fixed-interval loops (e.g., `1h` for a file sync).
- **Event-Driven:** Reactive to MCP notifications (e.g., a file change or system event).

### Testing

Native intents are tested using standard Go testing tools. This provides access to the full Go ecosystem for assertions, mocking, and coverage analysis.

```bash
go test ./...
```

## The Ecosystem

Clara ships with a variety of built-in and first-party MCP servers:

- **`chrome`:** Full browser automation (click, fill, navigate) via a companion
  extension.
- **`fs`:** Local filesystem management and change watching.
- **`db`:** SQLite tool for persistent intent data.
- **`llm`:** Multiplexed access to online providers (Gemini, etc.) and local
  models (via Ollama).
- **`macos`:** Native macOS access (Photos, Reminders, Calendar, etc) via `ClaraBridge`.
- **`zk`:** Specialized Zettelkasten/Obsidian vault tools.
- **`taskwarrior`:** Integration with the Taskwarrior CLI.
- **`tmux`:** Integration with the tmux CLI for managing terminal sessions.
- **`shell`:** Local command execution.
- **`search`:** Unified search for mail, local files (`mdfind`), etc.
- **`web`:** Internet search via DuckDuckGo.
- **`webex`:** Webex messaging and search.

## Project Structure

- `cmd/clara/`: The unified binary (CLI + Daemon).
- `cmd/intents/`: Source code for native Go plugin intents.
- `cmd/integrations/`: Source code for native Go plugin integrations.
- `internal/mcpserver/`: Built-in MCP implementations.
- `swift/`: Native macOS bridge.
- `extension/`: Chrome extension source.

---

_Clara is under active development._
