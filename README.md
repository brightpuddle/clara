# Clara

> "A reliable, lightweight supervisor for external scripts, rule-based triggers, and MCP tools."

Clara is an efficient, Go-based supervisor and integration bridge. It acts as a central hub that connects external sensor events, scheduled jobs, and long-running workers to external automation scripts (with a primary focus on Lua, alongside Python and shell binaries) while exposing a rich set of native and remote MCP (Model Context Protocol) tools.

---

## Core Philosophy

- **External Scripting:** Automation logic lives in clean, external scripts (Lua, Python, Bash) rather than internal domain-specific languages. External scripts interact with Clara's tools via standard MCP interfaces or CLI commands.
- **Smart Rule Triggers:** Event-driven automations use a declarative boolean AST (supporting `and`, `or`, `not`, dot-notation path extraction, regex, and comparison operators) to route incoming CloudEvents to specific scripts.
- **"Let It Fail" Supervision:** External processes are monitored with configurable timeouts, execution caps, and worker restart policies (`always`, `on_failure`, `never`).
- **Complete Audit Trail:** Every script execution (stdout, stderr, exit codes, duration) and MCP tool call is recorded to a local SQLite store for instant inspection.
- **Rich Observability:** Full browser-based Web Management UI (built with Go Templ, Tailwind CSS v4, DaisyUI v5, HTMX, and Alpine.js) and a fast Unix-socket CLI.

---

## Architecture

```
External Signals / Sensors
  (Webex, Discord, Email, Filesystem, CLI)
            │
            ▼
     ┌─────────────┐
     │  Event Bus  │ ◄─── CloudEvents
     └──────┬──────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│                    Trigger Manager                          │
│                                                             │
│  • Event Rules (Boolean AST: and/or/not, dot paths, regex)  │
│  • Schedule Triggers (Cron / Interval)                      │
│  • Supervised Workers (Restart policies & health tracking)  │
└───────────────────────────┬─────────────────────────────────┘
                            │ Spawns & Supervises
                            ▼
               ┌────────────────────────┐
               │    External Scripts    │
               │  (Lua, Python, Shell)  │
               └────────────┬───────────┘
                            │ Calls Tools
                            ▼
               ┌────────────────────────┐
               │    MCP Tool Server     │
               │ (Native & Remote Tools)│
               └────────────────────────┘
```

---

## Features

- **Trigger Engine (`internal/trigger`):**
  - **Event Triggers:** Match CloudEvents using smart rules (e.g. `data.mailbox == "inbox" and data.subject matches "Invoice"`).
  - **Schedule Triggers:** Standard 5/6-field cron expressions and intervals (e.g., `@every 5m`).
  - **Worker Triggers:** Supervised long-running background processes with automatic restart policies and backoff.
- **MCP Tool Catalog & Server (`internal/server`, `internal/registry`):**
  - Built-in MCP server (`/mcp/sse` and `/mcp/messages`) with HTTP bearer authentication.
  - Native tools for filesystem, SQLite database queries, shell execution, web search, Chrome browser automation, and macOS native integrations.
  - Integration plugin discovery and loading via `hashicorp/go-plugin`.
- **Audit & Persistence (`internal/store`):**
  - SQLite backend recording all script runs, outputs, errors, durations, and tool invocation history.
- **Web UI (`/ui/`):**
  - Live dashboards for Triggers, Execution Runs, MCP Tools & Integrations, System Logs, and YAML Configuration.
- **Unix-Socket IPC CLI:**
  - Fast, responsive CLI interface communicating with the running daemon over domain sockets.

---

## Getting Started

### Installation & Build

Prerequisites: Go 1.24+, Node.js/pnpm, `templ`.

```bash
# Clone the repository
git clone https://github.com/brightpuddle/clara.git
cd clara/agent

# Install frontend dependencies and build
pnpm install
make build

# Optional: Install as a macOS LaunchAgent
make install
```

### Running the Daemon

```bash
# Run in the foreground
clara serve

# Or manage the LaunchAgent daemon
clara agent start
clara agent status
clara agent logs -f
```

---

## Defining Triggers

Triggers are configured in YAML files inside your task directory (default: `~/.config/clara/tasks/` or configured `task_dirs`).

### 1. Smart Event Trigger (Lua Script)

```yaml
# ~/.config/clara/tasks/email_filter.yaml
id: email-invoice-processor
name: Email Invoice Processor
description: Routes invoice emails to an automated Lua processing script
type: event
match:
  and:
    - field: type
      op: equals
      value: email.received
    - field: data.mailbox
      op: equals
      value: inbox
    - or:
        - field: data.subject
          op: contains
          value: Invoice
        - field: data.subject
          op: regex
          value: "(?i)receipt|bill"
action:
  exec: lua /Users/nathan/scripts/process_invoice.lua
  pass_event: stdin
  timeout: 30s
```

### 2. Scheduled Trigger

```yaml
# ~/.config/clara/tasks/daily_cleanup.yaml
id: daily-disk-cleaner
name: Daily Disk Cleaner
description: Cleans temporary files every midnight
type: schedule
schedule: "0 0 * * *"
action:
  exec: python3 /Users/nathan/scripts/clean_temp.py
  timeout: 5m
```

### 3. Supervised Worker

```yaml
# ~/.config/clara/tasks/discord_bot.yaml
id: discord-bot-worker
name: Discord Bot Worker
description: Supervised long-running event listener
type: worker
action:
  exec: /Users/nathan/bin/discord-worker
  restart: always
  restart_delay: 5s
  max_restarts: 10
```

---

## External Scripting with Lua

Scripts receive event data via `stdin` (default), environment variables, or CLI arguments. Here is an example Lua script using Clara's MCP tools:

```lua
-- scripts/process_invoice.lua
local json = require("json") -- or any standard lua JSON parser

-- Read CloudEvent from Clara on stdin
local raw_event = io.read("*a")
local event = json.decode(raw_event)

print(string.format("Processing invoice from: %s", event.data.sender))

-- Perform work or invoke Clara MCP endpoints...
```

---

## CLI Reference

```bash
# Daemon Management
clara serve                         # Start daemon in foreground
clara status                        # Show daemon trigger/tool summary
clara agent {start,stop,status}     # Manage LaunchAgent

# Triggers
clara trigger list                  # List all configured triggers
clara trigger show <id>             # Show trigger details and match rules
clara trigger run <id>              # Manually trigger a script execution

# Execution Runs & Audit
clara run list [-n 20]              # View recent script execution runs
clara run show <id>                 # Inspect stdout, stderr, exit code, and event payload

# Tools & MCP
clara tool list                     # List all registered MCP tools
clara tool show <name>              # Show tool JSON schema
clara tool call <name> -a '<json>'  # Execute a tool directly
clara mcp list                      # List configured external MCP servers

# Events
clara event emit --type=<t> -d '<json>'  # Emit a CloudEvent onto the event bus
clara event logs -f                      # Stream live events from the bus
```

---

## Web Management UI

When the HTTP server is enabled in `~/.config/clara/config.yaml`:

```yaml
server:
  listen_addr: "127.0.0.1:3333"
  shared_secret: "your-auth-token"
```

Access the UI at `http://127.0.0.1:3333/ui/`:
- **/ui/triggers:** Inspect configured event rules, cron schedules, and worker statuses.
- **/ui/runs:** Full history of script executions, exit codes, and output logs.
- **/ui/integrations:** Overview of connected sensors, plugins, and MCP servers.
- **/ui/logs:** Live streaming daemon log viewer.
- **/ui/config:** In-browser structured and raw YAML configuration editor.

---

## License

MIT
