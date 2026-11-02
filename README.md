# Clara

> "A reliable, lightweight supervisor for external scripts, rule-based triggers, and MCP tools."

Clara is an efficient, Go-based supervisor and integration bridge. It acts as a central hub that connects external sensor events, scheduled jobs, and long-running workers to external automation scripts (with a primary focus on Lua, alongside Python and shell binaries) while exposing a rich set of native and remote MCP (Model Context Protocol) tools.

---

## Core Philosophy

- **External Scripting:** Automation logic lives in clean, external scripts (TypeScript via Bun, Python, Bash) rather than internal domain-specific languages. External scripts interact with Clara's tools via standard MCP interfaces or CLI commands.
- **Smart Rule Triggers:** Event-driven automations use a declarative boolean AST (supporting `and`, `or`, `not`, dot-notation path extraction, regex, and comparison operators) to route incoming CloudEvents to specific scripts.
- **"Let It Fail" Supervision:** External processes are monitored with configurable timeouts, execution caps, and worker restart policies (`always`, `on_failure`, `never`).
- **Complete Audit Trail:** Every script execution (stdout, stderr, exit codes, duration) and MCP tool call is recorded to a local SQLite store for instant inspection.
- **Conversational & Terminal Control Plane:** Native bidirectional integration with Discord and Webex replaces bulky web UIs. Multiplexes tmux development sessions into dedicated Discord channels (`#eve-*`, `#sophia-*`) and supports satellite worker nodes over reverse SSH tunnels.

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
               │  (TypeScript via Bun)  │
               └────────────┬───────────┘
                            │ Calls Tools
                            ▼
               ┌────────────────────────┐
               │    MCP Tool Server     │
               │ (Native & Satellite)   │
               └────────────────────────┘
```

---

## Features

- **Trigger Engine (`internal/trigger`):**
  - **Event Triggers:** Match CloudEvents using smart rules (e.g. `data.mailbox == "inbox" and data.subject matches "Invoice"`).
  - **Schedule Triggers:** Standard 5/6-field cron expressions and intervals (e.g., `@every 5m`).
  - **Worker Triggers:** Supervised long-running background processes with automatic restart policies and backoff.
- **MCP Tool Catalog & Server (`internal/server`, `internal/registry`):**
  - Built-in MCP server (`/mcp` Streamable HTTP, `/mcp/sse`, and `/mcp/messages`) with HTTP bearer authentication.
  - Native tools for filesystem, SQLite database queries, shell execution, web search, Chrome browser automation, and macOS native integrations.
  - Integration plugin discovery and loading via `hashicorp/go-plugin`.
  - Ambient TypeScript type stub generator (`clara types gen`) providing full autocomplete and compile-time verification for external scripts.
- **Audit & Persistence (`internal/store`):**
  - SQLite backend recording all script runs, outputs, errors, durations, and tool invocation history.
- **Multi-Host Topology:**
  - **Coordinator Hub (`eve`):** M4 Mac mini (64GB) running central event bus, Discord/Webex bots, and trigger evaluations.
  - **VPN Satellite (`sophia`):** M4 MacBook Pro (24GB) providing Cisco VPN access, mounted dynamically under the `laptop.*` MCP namespace via reverse SSH tunnels.
- **Unix-Socket IPC CLI:**
  - Fast, responsive CLI interface communicating with the running daemon over domain sockets.

---

## Getting Started

### Installation & Build

Prerequisites: Go 1.24+, Bun (for TypeScript scripts).

```bash
# Clone the repository
git clone https://github.com/brightpuddle/clara.git
cd clara

# Compile the core daemon and integration plugins
make build

# Install as a macOS LaunchAgent
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

### 1. Smart Event Trigger (TypeScript / Bun)

```yaml
# ~/.config/clara/tasks/email_filter.yaml
id: email-invoice-processor
name: Email Invoice Processor
description: Routes invoice emails to an automated TypeScript processing script
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
  exec: bun
  args: ["run", "scripts/process_invoice.ts"]
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
  exec: bun
  args: ["run", "scripts/clean_temp.ts"]
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
  exec: bun
  args: ["run", "scripts/discord_worker.ts"]
  restart: always
  restart_delay: 5s
  max_restarts: 10
```

---

## External Scripting with TypeScript (Bun)

Scripts receive event data via `stdin` (default), environment variables, or CLI arguments. Here is an example TypeScript script using Clara's MCP tools:

```typescript
// scripts/process_invoice.ts
import { readEvent, callTool, runID } from "./clara";

interface InvoiceData {
  sender: string;
  amount: number;
  pdfPath?: string;
}

// Read incoming CloudEvent from Clara on stdin
const event = await readEvent<InvoiceData>();
console.log(`[Run ${runID()}] Processing invoice from: ${event.data.sender}`);

// Invoke Clara MCP tool with full type safety
await callTool("notify.send", {
  message: `Received invoice from ${event.data.sender} ($${event.data.amount})`,
});
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
clara types gen [--out path]        # Auto-generate TypeScript definitions (.d.ts)
clara mcp list                      # List configured external MCP servers
clara mcp serve --profile <name>    # Expose a whitelisted tool subset as a stdio MCP server

# Events
clara event emit --type=<t> -d '<json>'  # Emit a CloudEvent onto the event bus
clara event logs -f                      # Stream live events from the bus
```

---

## Conversational Control Plane & Multiplexer

Clara uses Discord and Webex as its primary control surfaces instead of a static web browser UI:

- **Discord Guild Setup:**
  - **`#agents`**: Central orchestration channel for checking `status`, `sync`, `sessions`, `runs`, `triggers`, and running `/code` tasks.
  - **`#qa`**: Dedicated channel for Cisco engineering research, meeting transcripts, and weekly rituals (`timesheet`, `myperformance`).
  - **`#home`**: Personal notes, Obsidian sync, reminders, and family life.
  - **`#notify`**: Stream of automated triage alerts and digest cards.
  - **`#eve-<name>` / `#sophia-<name>`**: Active `tmux` terminal channels created dynamically via `sync`. Type directly into Discord or dictate voice notes to send keystrokes to terminal agents.

---

## License

MIT
