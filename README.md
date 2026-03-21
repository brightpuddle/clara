# Clara

Clara is a local-first agentic orchestrator for macOS.

It runs as a Go daemon that:

- starts and supervises MCP servers
- exposes their tools through a unified registry
- watches a tasks directory for `.star` Starlark intent files
- executes those intents with persisted run history and wait/resume support
- provides a CLI for daemon control, intent management, and tool inspection

Clara is intentionally MCP-first:

> If a capability is available to intents, it should be delivered through MCP.

That keeps the daemon focused on orchestration, policy, lifecycle, and
persistence instead of accumulating bespoke direct integrations.

Clara can also act as an MCP gateway:

- as an MCP client, it starts and aggregates configured MCP server subprocesses
  for local intent execution
- as an MCP server, `clara gateway` re-exposes that unified tool registry over
  stdio for external agents

## Table of contents

- [Getting started](#getting-started)
- [Architecture at a glance](#architecture-at-a-glance)
- [CLI usage](#cli-usage)
- [Configuration](#configuration)
- [Intent files (`.star`)](#intent-files-star)
- [Supported features](#supported-features)
- [Operational model](#operational-model)
- [Project structure](#project-structure)
- [Development notes](#development-notes)

## Getting started

### Requirements

- macOS
- Go 1.26+
- Swift 6.0+ if you want to build the native macOS bridge
- Any external MCP servers or local tools you reference in your config

### Build Clara

```bash
go build ./cmd/clara
```

This produces the `clara` binary for local use.

### Create your config

Copy the example config into the default config location:

```bash
mkdir -p ~/.config/clara
cp config.yaml.example ~/.config/clara/config.yaml
```

Edit `~/.config/clara/config.yaml` so `mcp_servers` contains the MCP services
you want Clara to manage.

If Clara runs under launchd and your MCP server commands live outside the
default system path, set `mcp_command_search_paths` to include those binary
directories (for example `${HOME}/go/bin`). Clara prepends those paths when
resolving bare MCP commands and when constructing the subprocess `PATH`.

### Install Clara as a LaunchAgent

For local development on macOS, the easiest way to install or update Clara is:

```bash
make install
```

This will:

- build `clara`
- copy the binary to `/usr/local/bin/clara`
- install `com.brightpuddle.clara.agent.plist` to `~/Library/LaunchAgents`
- restart the agent if it is already running, or start it if it is not

After that, you can manage the daemon with:

```bash
clara agent start
clara agent stop
clara agent status
clara agent logs
clara agent logs --watch
```

The launchd-managed daemon writes to `~/.local/share/clara/clara.log`. That log
is application-managed and rotates automatically, so you can safely `tail -f` it
during development without it growing forever. By default Clara keeps the active
log plus a small set of rotated backups.

To remove the local install again:

```bash
make uninstall
```

### Start the daemon manually

```bash
clara serve
```

The daemon will:

- create its runtime data directory
- open its internal SQLite database
- start configured MCP servers
- watch the tasks directory for `.star` intents
- expose the control socket used by the CLI

If one configured MCP server fails to start, Clara now logs the error and
continues starting the remaining MCP servers.

### Check the daemon

```bash
clara agent status
clara intent list
clara tool list
```

### Create your first intent

Create a file in the watched tasks directory:

```text
~/.local/share/clara/tasks/hello.star
```

Example:

```python
init(
    id = "hello-world",
    description = "List the current directory once",
)

def main():
    return tool("fs.list_directory", path = ".")
```

For an `on_demand` intent, start it manually:

```bash
clara intent start hello-world
clara intent logs hello-world
```

You can also run a one-off `.star` file directly without installing it into the
tasks directory:

```bash
clara run ./path/to/hello.star
```

## Architecture at a glance

Clara has five main pieces:

### 1. The daemon

The daemon is responsible for:

- config loading
- MCP subprocess lifecycle
- watching the tasks directory
- executing intents
- storing runs, events, wait state, and replay history

### 2. MCP registry

Configured MCP servers are connected and their tools are exposed through a
single registry.

That means intents always talk to tools through MCP-facing names such as:

- `fs.list_directory`
- `db.query`
- `taskwarrior.task_add`

That same registry can also be surfaced externally through `clara gateway`,
which publishes the aggregated toolset as one MCP server on stdio.

### 3. Starlark intent compiler

Clara compiles `.star` files into validated runtime intents.

That compilation pass:

- requires a top-level `init(...)` call
- requires a callable `main()`
- extracts metadata such as `id`, `mode`, `schedule`, and `trigger`
- stores the original script source for execution and replay

### 4. Starlark runtime

At execution time Clara provides builtins such as:

- `tool(...)` to invoke registered tools
- `wait(...)` to pause a run and persist its wait state

### 5. Internal store

Clara keeps a private SQLite database for:

- installed run state
- run events
- wait metadata
- deterministic replay history for Starlark workflows

This database is for Clara's own orchestration state. It is not the same thing
as the user-facing SQLite MCP server.

## CLI usage

### Top-level commands

| Command              | Description                                            |
| -------------------- | ------------------------------------------------------ |
| `clara`              | Launch the interactive TUI/HUD                         |
| `clara serve`        | Start the Clara agent in the foreground                |
| `clara agent start`  | Start the LaunchAgent-managed daemon in the background |
| `clara agent stop`   | Gracefully stop the daemon and unload its LaunchAgent  |
| `clara agent status` | Show daemon status                                     |
| `clara agent logs`   | Show the recent daemon log output                      |
| `clara intent ...`   | Manage installed intents and one-off intent runs       |
| `clara tool ...`     | Inspect or call registered tools                       |
| `clara gateway`      | Start an aggregated MCP gateway on stdio               |
| `clara mcp ...`      | Start built-in MCP servers on stdio                    |

### Intent commands

| Command                                      | Description                                                                              |
| -------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `clara intent list`                          | List installed intents with mode and lifecycle state                                     |
| `clara intent start <id> [task]`             | Start an intent task (fires a run for on-demand; activates loop for schedule/worker/event) |
| `clara intent start <id> --input '<json>'`   | Deliver JSON input to the latest waiting run for that intent                             |
| `clara intent stop <id> [task]`              | Stop a managed `schedule`, `worker`, or `event` intent and cancel its latest waiting run |
| `clara intent logs [id]`                     | Stream run activity                                                                      |
| `clara intent resume <run-id>`               | Resume a paused Starlark run directly by run ID                                          |
| `clara intent run <file.star>`               | Run a `.star` file once without installing it                                            |

### Tool commands

| Command                                | Description                         |
| -------------------------------------- | ----------------------------------- |
| `clara tool list`                      | List tool providers                 |
| `clara tool list <server>`             | List tools for one provider prefix  |
| `clara tool show <tool>`               | Show a tool schema                  |
| `clara tool call <tool> key=value ...` | Call a tool directly and print JSON |

### Built-in MCP servers

| Command                     | Description                                        |
| --------------------------- | -------------------------------------------------- |
| `clara mcp fs`              | Filesystem MCP server, including `wait_for_change` |
| `clara mcp db [path]`       | SQLite MCP server                                  |
| `clara mcp ollama`          | Ollama MCP server                                  |
| `clara mcp taskwarrior`     | Taskwarrior MCP server                             |
| `clara mcp zk <vault-path>` | Zettelkasten Markdown vault MCP server             |
| `clara mcp chrome`          | Chrome browser automation MCP server               |

`clara mcp ollama-embeddings` accepts `--model` and `--url` flags and defaults
to `nomic-embed-text` and `http://localhost:11434`.

The `zk` server indexes an Obsidian-style Markdown vault at startup (following
symlinks up to 5 levels), and exposes tools for note CRUD, wikilink resolution,
tag queries, and frontmatter access:

| Tool                   | Description                                                    |
| ---------------------- | -------------------------------------------------------------- |
| `note_list`            | List all notes with metadata                                   |
| `note_get`             | Get content and metadata for a note by name or path           |
| `note_create`          | Create a new note                                              |
| `note_update`          | Overwrite an existing note                                     |
| `note_delete`          | Delete a note                                                  |
| `note_resolve_wikilink` | Resolve `[[wikilink]]` target to absolute path               |
| `tag_list`             | List all tags with note counts                                 |
| `tag_notes`            | Find all notes with a given tag                                |
| `vault_reload`         | Rebuild the in-memory index from disk                          |

Configure it in `config.yaml`:

```yaml
mcp_servers:
  - name: zk
    command: clara
    args: [mcp, zk, ~/notes]
    description: "Zettelkasten vault"
```

`clara mcp chrome` requires a one-time setup to load the companion extension.
It bridges browser tool calls to the Clara Chrome extension over a local
WebSocket (`localhost:48765`). When Clara starts, `clara mcp chrome` is
launched as a subprocess and listens for the extension to connect.

Setup:

1. `make install` — build and install the `clara` binary
2. Open Chrome → `chrome://extensions/` → enable **Developer mode** →
   **Load unpacked** → select the `extension/` directory in the Clara repo

Tools exposed by `clara mcp chrome`:

| Tool                    | Description                                              |
| ----------------------- | -------------------------------------------------------- |
| `browser_navigate`      | Open a URL in a new background tab or an existing tab    |
| `browser_click`         | Click an element by CSS selector                         |
| `browser_fill`          | Fill a text input (React-compatible)                     |
| `browser_upload_file`   | Set a file on `<input type="file">` via CDP              |
| `browser_screenshot`    | Capture visible tab area as a PNG data URL               |
| `browser_read_page`     | Return page title, URL, and visible text                 |
| `browser_get_tabs`      | List open tabs, optionally filtered by URL pattern       |
| `browser_close_tab`     | Close a tab by ID                                        |
| `browser_wait_for_load` | Wait until a tab's document status is `complete`         |

Enable in `config.yaml`:

```yaml
mcp_servers:
  - name: chrome
    command: clara
    args: [mcp, chrome]
    description: "Chrome browser automation"
```

### Typical workflow

```bash
make install
clara agent logs --watch
clara tool list
clara intent list
clara intent start hello-world
clara intent logs hello-world
```

### Using Clara as an MCP gateway

Run:

```bash
clara gateway
```

This starts a stdio MCP server that:

- loads your normal Clara config
- starts each configured MCP server
- aggregates all discovered tools into one MCP surface
- serves that combined toolset to external MCP clients

This is useful when you want tools like Claude Code or Aider to talk to Clara
through a single connection instead of managing each local MCP server
separately.

### TUI notes

- Slash-command history is persisted across sessions in Clara's data directory
  and can be navigated with the up/down arrows.
- `/tool call` autocomplete completes provider IDs first, then tool suffixes
  after `provider.`.
- Entering `/tool call <provider>` lists that provider's tools without requiring
  a full `provider.tool` name.

## Configuration

Clara reads config from:

```text
~/.config/clara/config.yaml
```

If `data_dir` is not overridden, Clara uses:

```text
~/.local/share/clara
```

Important derived paths:

- internal DB: `~/.local/share/clara/clara.db`
- control socket: `~/.local/share/clara/clara.sock`
- dynamic MCP socket: `~/.local/share/clara/clara-mcp.sock`
- tasks directory: `~/.local/share/clara/tasks`
- log path: `~/.local/share/clara/clara.log`

### Configuration file shape

```yaml
log_level: info

mcp_servers:
  - name: fs
    command: clara
    args: [mcp, fs]
    description: "Built-in filesystem server"

  - name: db
    command: clara
    args: [mcp, db, ${HOME}/.local/share/clara/data.db]
    description: "Built-in SQLite tool server"

  - name: ollama
    command: clara
    args: [mcp, ollama-embeddings, --model, nomic-embed-text, --url, http://localhost:11434]
    description: "Built-in Ollama embeddings server"

  - name: bridge
    command: /usr/local/bin/ClaraBridge
    args: []
    description: "Native macOS MCP server"
```

### Supported config fields

#### `log_level`

Supported values:

- `trace`
- `debug`
- `info`
- `warn`
- `error`

#### `data_dir`

Overrides the default runtime directory.

#### `mcp_servers`

Each entry defines an MCP subprocess that Clara manages.

Fields:

- `name`: registry prefix used by tools
- `command`: executable to launch
- `args`: command-line arguments
- `env`: optional environment variables, with `${VAR}` expansion
- `description`: human-readable description

### Environment expansion

All string values support `${ENV_VAR}` expansion.

Example:

```yaml
env:
  OPENAI_API_KEY: ${OPENAI_API_KEY}
```

## Intent files (`.star`)

Clara uses `.star` (Starlark) files as the authored intent format.

JSON, YAML, and Markdown are not supported as authored intent sources.

### Intent ID

The intent ID is always derived from the filename stem. A file named
`reminders-sync.star` has intent ID `reminders-sync`. There is no `id` field
inside the file.

### Required structure

Every intent file must either:

- define a callable `main()` (implicitly registered as an on-demand task), or
- register at least one task with `task(...)`

Minimal example — a single on-demand intent backed by `main()`:

```python
# hello-world.star  →  intent id: "hello-world"
def main():
    return tool("fs.list_directory", path = ".")
```

### `describe(...)`

Optional. Sets a human-readable description for the intent.
May only be called once per file.

```python
describe("React to reminder changes and run nightly syncs")
```

### `task(handler, *, trigger=, schedule=, interval=)`

Registers a task handler. The execution mode is inferred from the arguments:

| Argument   | Mode       | Description                                      |
| ---------- | ---------- | ------------------------------------------------ |
| `trigger`  | `event`    | MCP notification name, e.g. `bridge.reminders_changed` |
| `schedule` | `schedule` | Cron-style expression, e.g. `0 7 * * *`          |
| `interval` | `worker`   | Go duration string, e.g. `10m` or `1h`           |
| (none)     | `on_demand` | Idle until triggered via CLI                    |

The `handler` argument is required and must be a callable defined in the file.
It may be passed positionally or as a keyword argument.

Mode inference rules:

- `trigger` present → `event`
- `schedule` present → `schedule`
- `interval` present → `worker`
- none of the above → `on_demand`

Multiple `task(...)` calls in the same file register multiple handlers within a
single intent:

```python
# reminders-sync.star
describe("React to reminder changes and run nightly syncs")

def on_reminder_change(event):
    return tool("taskwarrior.sync_reminder", reminder = event["item"])

def nightly_sync():
    return tool("taskwarrior.full_sync")

task(on_reminder_change, trigger = "bridge.reminders_changed")
task(nightly_sync, schedule = "0 2 * * *")
```

### `main()`

`main()` is the default runtime entrypoint for on-demand intents.

If no `task(...)` calls are present and `main()` is defined, Clara
automatically registers `main` as an on-demand task — no explicit
`task(main)` is required.

When used with `task(...)`, `main()` is just another handler and must be
explicitly registered if you want it to be callable:

```python
task(main)                     # on_demand
task(main, schedule = "0 7 * * *")  # scheduled
```

### Builtins available at runtime

#### `tool(name, **kwargs)`

Calls a registered Clara/MCP tool.

Example:

```python
def main():
    return tool("fs.list_directory", path = ".")
```

#### `wait(name, **kwargs)`

Pauses execution and stores a wait request.

The run can later be resumed:

- by run ID using `clara intent resume <run-id>`
- by installed intent ID using `clara intent start <id> --input '<json>'`

Example:

```python
# review-gate.star
describe("Gate a release on human approval")

def main():
    approval = wait("approval", prompt = "Ship this release?")
    if approval.get("approved"):
        return tool("fs.write_file", path = "/tmp/release.txt", content = "approved")
    return {"status": "rejected"}
```

For push-style integrations, Clara also supports blocking MCP tools that wait
until an external event arrives and then return structured data. Current
built-in examples are:

- `fs.wait_for_change` for filesystem create/change/delete events
- `bridge.reminders_wait_change` for Reminders create/update/delete events
- `bridge.events_wait_change` for Calendar create/update/delete events

Those tools are useful when you want a linear script to pause inside a tool call
rather than use Clara's persisted `wait(...)` / `trigger --input` path.

### Runtime modes

#### `on_demand`

Installed but idle until started.

```bash
clara intent start <id>
```

When a file has multiple on-demand tasks, specify the handler name:

```bash
clara intent start <id> <handler>
```

#### `schedule`

Runs automatically based on a cron-style schedule.

Supported cron syntax is the standard 5-field form:

```text
minute hour day month weekday
```

Supported field patterns:

- `*`
- exact values such as `7`
- ranges such as `1-5`
- lists such as `1,2,3`
- step values such as `*/5` or `1-10/2`

Example:

```python
# daily-weather.star
describe("Check the forecast each morning")

def main():
    weather = tool("weather.forecast_today")
    if weather.get("rain_expected"):
        return tool("bridge.notify", title = "Weather", body = "Bring an umbrella")
    return weather

task(main, schedule = "0 7 * * *")
```

#### `worker`

Runs immediately, then repeats on a fixed interval.

`interval` uses Go duration syntax — `10s`, `5m`, `1h`.

Example:

```python
# note-indexer.star
describe("Continuously re-index notes")

def main():
    notes = tool("fs.search_files", path = "~/Notes", pattern = "*.md")
    return tool("indexer.sync_notes", files = notes)

task(main, interval = "10m")
```

#### `event`

Runs a specific handler each time a matching MCP notification arrives.

The `trigger` value must match the fully-qualified notification name Clara
receives from an MCP server, such as `bridge.reminders_changed` or
`bridge.events_changed`.

Example:

```python
# reminders-triage.star
describe("React to reminder change notifications")

def on_reminder_change(event):
    return tool("taskwarrior.sync_reminder", reminder = event["item"])

task(on_reminder_change, trigger = "bridge.reminders_changed")
```

### Practical structure guidance

Good intent structure usually looks like this:

1. optional: call `describe("...")` once at the top
2. define small helper functions
3. define handlers (`main`, or named functions)
4. call `task(...)` to register each handler with its scheduling/trigger
5. use `wait(...)` when the script should suspend and later resume

Example:

```python
# morning-review.star
describe("Collect daily review data")

def summarize(tasks, notes):
    return {
        "task_count": len(tasks),
        "note_count": len(notes),
    }

def main():
    tasks = tool("taskwarrior.list_tasks")
    notes = tool("fs.search_files", path = "~/Notes", pattern = "*.md")
    return summarize(tasks, notes)

task(main, schedule = "30 8 * * 1-5")
```

## Supported features

Clara currently supports:

- `.star` intent authoring
- intent ID derived from filename; optional `describe("...")` for human label
- `main()` as the default Starlark entrypoint (auto-registered as on-demand)
- multi-handler `.star` intents via top-level `task(...)` calls
- one-off intent execution with `clara intent run`
- installed intent management through the daemon
- four runtime modes: `on_demand`, `schedule`, `worker`, `event` (inferred from args)
- persisted Starlark wait/resume behavior
- event-style input delivery through `trigger --input`
- MCP notification-driven event handlers for managed tasks
- run/event persistence in SQLite
- tool discovery and direct tool calls from the CLI
- built-in MCP servers for filesystem, SQLite, Ollama embeddings, and
  Taskwarrior
- blocking MCP wait tools for filesystem, Reminders, and Calendar change events
- persisted TUI slash-command history with provider-aware `/tool call`
  autocomplete
- `updated_after` filtering on `bridge.reminders_list` and
  `taskwarrior.list_tasks`
- external MCP server composition through config

## Operational model

### Installed intents

An intent is "installed" when its `.star` file exists in the watched tasks
directory.

`clara intent list` shows one row per task:

- the intent ID (from filename)
- handler name
- mode
- schedule/interval/trigger detail
- whether the intent is currently active

### Run persistence

Every execution is persisted with:

- a run ID
- current state
- status
- event history
- wait metadata
- replay history

### Wait/resume model

Clara does not keep paused Starlark interpreters alive in memory indefinitely.

Instead it:

1. executes until `wait(...)`
2. stores the wait request and replay history
3. exits the current execution
4. resumes later by replaying prior deterministic steps plus the new wait result

This makes pause/resume durable across restarts and easier to inspect.

### Current event-mode caveat

`event` mode is implemented through the persisted wait/resume model.

Today:

- event intents can auto-start and pause on `wait(...)`
- `trigger --input` can resume the latest waiting run
- an event intent does not automatically re-arm itself after a resumed run
  finishes

If you want a fresh waiting run after handling an event, start or trigger the
intent again.

## Project structure

```text
github.com/brightpuddle/clara/
├── cmd/clara/                  # Unified binary: daemon, CLI, built-in MCP launchers
├── internal/config/            # Config loading and derived paths
├── internal/interpreter/       # State machine + Starlark execution
├── internal/ipc/               # Control socket protocol
├── internal/mcpserver/         # Built-in MCP servers
│   ├── chrome/                 # Chrome browser automation (navigate, click, fill, upload)
│   ├── db/                     # SQLite MCP server
│   ├── fs/                     # Filesystem MCP server
│   ├── ollama/                 # Ollama embeddings/generation MCP server
│   ├── taskwarrior/            # Taskwarrior MCP server
│   └── zk/                     # Zettelkasten vault MCP server
├── internal/orchestrator/      # Compiled intent model + .star loading
├── internal/registry/          # MCP server connections and tool registry
├── internal/store/             # Internal SQLite persistence
├── internal/supervisor/        # Tasks directory watcher and runtime mode lifecycle
├── extension/                  # Clara Chrome extension (load unpacked in Chrome)
├── swift/                      # Native macOS bridge MCP server
├── config.yaml.example         # Example daemon configuration
└── README.md                   # This manual
```

## Development notes

### Testing

Run the test suite with:

```bash
go test ./...
```

### Formatting

Use the repository's standard Go formatting workflow before committing.

### Documentation maintenance

When you change Clara's features or architecture:

- update `README.md`
- update `.github/copilot-instructions.md`

These two documents should stay aligned with the actual implementation.
