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
- **Efficiency:** Running a deterministic script is orders of magnitude faster
  and cheaper than prompting an LLM for every step.
- **Inspectability:** You can diff, version, and improve your scripts over time
  using standard software engineering tools.
- **Durable State:** Clara manages long-running tasks that can wait for human
  input or external events without keeping a "hot" LLM context alive.

## Architecture at a Glance

Clara is built on three core pillars:

1. **Integration Plugins (go-plugin):** Capabilities such as Filesystem, Chrome,
   SQLite, LLM, and macOS-native APIs are delivered as standalone Go binaries
   built with [`hashicorp/go-plugin`](https://github.com/hashicorp/go-plugin).
   Each integration exposes a set of named tools to the daemon over a
   net/RPC (or gRPC, for the Swift bridge) connection. Tools are registered into
   a central **Registry** keyed by `namespace.tool_name`.
2. **Starlark Intents (`.star` files):** High-level workflows are authored as
   [Starlark](https://github.com/google/starlark-go) scripts. The daemon
   discovers `.star` files in `~/.config/clara/tasks/`, hot-reloads them on
   change, and executes them via a built-in Starlark interpreter. Scripts call
   integration tools through namespace proxies (`fs.read_file(...)`,
   `llm.complete(...)`, etc.) and can `clara.wait(...)` for external events,
   enabling durable, resumable workflows.
3. **The Daemon:** A Go-based background service that loads integration plugins,
   watches the tasks directory for intent scripts, schedules and executes
   intents, and persists run state in a local SQLite store.

> **Note on MCP:** `mcp-go` is used *inside* integration plugins to describe and
> dispatch individual tools. It is an implementation detail, not the transport
> between the daemon and plugins (that transport is go-plugin RPC/gRPC).

## Getting Started

### Installation

For a streamlined installation on macOS:

```bash
curl -s https://raw.githubusercontent.com/brightpuddle/clara/main/scripts/install.sh | bash
```

This installs the `clara` binary, the built-in integration plugins, the
`ClaraBridge` (for native macOS integrations like Photos/Reminders), and the
companion Chrome extension.

### Your First Intent

Intents are Starlark scripts (`.star` files). Place them in
`~/.config/clara/tasks/` and the daemon picks them up automatically.

```python
# ~/.config/clara/tasks/hello.star

def main():
    result = shell.run(command="echo hello, world")
    print(result)
```

Run an intent immediately:

```bash
clara intent start hello
clara intent logs hello
```

## Writing Intents

Intents are Starlark scripts that call registered tools. They support several
execution modes declared in a `clara.describe(...)` / `clara.task(...)` header:

- **On-Demand:** Triggered manually via CLI or TUI.
- **Scheduled:** Cron-style execution (e.g., `0 7 * * *` for a morning brief).
- **Worker:** Fixed-interval loops (e.g., `1h` for a file sync).
- **Event-Driven:** Reactive to integration notifications (e.g., a file change).

### Calling Tools

Integration tools are available as Starlark namespace objects:

```python
def main():
    # Filesystem
    content = fs.read_file(path="/tmp/notes.txt")

    # LLM
    summary = llm.complete(prompt="Summarize: " + content)

    # Write result back
    fs.write_file(path="/tmp/summary.txt", content=summary)
```

Hyphens in tool names are mapped to underscores in Starlark
(`note-search` → `note_search`).

### Waiting for External Events

Scripts can pause and resume durably using `clara.wait(...)`:

```python
def main():
    response = clara.wait("user_confirmation", {"message": "Proceed?"})
    if response["confirmed"]:
        shell.run(command="make deploy")
```

### Testing

Clara re-executes intents from a recorded history on resume (deterministic
replay). To test intent logic locally, run the intent directly and inspect
the step log:

```bash
clara intent run my-intent --dry-run
clara intent logs my-intent
```

## Writing Integrations

Integrations are standalone Go binaries in `cmd/integrations/<name>/` that
implement `contract.Integration` from `pkg/contract` and are served via
`hashicorp/go-plugin`.

```go
type MyPlugin struct{}

func (p *MyPlugin) Configure(config []byte) error { ... }
func (p *MyPlugin) Description() (string, error)  { return "My integration", nil }
func (p *MyPlugin) Tools() ([]byte, error)         { /* return []mcp.Tool as JSON */ }
func (p *MyPlugin) CallTool(name string, args []byte) ([]byte, error) { ... }

func main() {
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: contract.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "myintegration": &contract.GenericIntegrationPlugin{Impl: &MyPlugin{}},
        },
    })
}
```

Install the compiled binary to `~/.config/clara/integrations/` and the daemon
will load it automatically on startup (or on `clara plugin load myintegration`).

Integration configuration is passed via `~/.config/clara/config.yaml`:

```yaml
integrations:
  myintegration:
    api_key: ${MY_API_KEY}
```

## The Ecosystem

Clara ships with a variety of built-in first-party integrations:

- **`chrome`:** Full browser automation (click, fill, navigate) via a companion
  extension.
- **`fs`:** Local filesystem management and change watching.
- **`db`:** SQLite tool for persistent intent data.
- **`llm`:** Multiplexed access to online providers (Gemini, etc.) and local
  models (via Ollama).
- **`macos`:** Native macOS access (Photos, Reminders, Calendar, etc.) via
  `ClaraBridge` (Swift gRPC).
- **`zk`:** Specialized Zettelkasten/Obsidian vault tools.
- **`shell`:** Local command execution.
- **`web`:** Internet search via DuckDuckGo.

## Project Structure

```
cmd/
  clara/            # Unified binary (CLI + Daemon)
  integrations/     # Built-in integration plugins (go-plugin RPC)
    fs/             # Filesystem
    db/             # SQLite
    llm/            # LLM multiplexer
    shell/          # Shell execution
    web/            # Web search
    chrome/         # Browser automation
    zk/             # Zettelkasten vault
internal/
  config/           # Config loader (~/.config/clara/config.yaml)
  orchestrator/     # Intent types, Starlark↔Go value helpers
  registry/         # Unified tool registry (namespace.tool → callable)
  interpreter/      # State machine executor + Starlark executor
  supervisor/       # Intent lifecycle (scheduling, event dispatch)
  store/            # SQLite persistence (runs, history, checkpoints)
  tui/              # Interactive TUI (bubbletea)
  ipc/              # Unix-socket IPC between CLI and daemon
pkg/
  contract/         # go-plugin RPC/gRPC contracts and handshake
swift/              # ClaraBridge — Swift gRPC integration for macOS APIs
extension/          # Chrome extension source
```

---

_Clara is under active development._
