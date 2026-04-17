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
- **Efficiency:** Running a Starlark script is orders of magnitude faster and
  cheaper than prompting an LLM for every step.
- **Inspectability:** You can diff, version, and improve your rulesets over
  time.
- **Durable State:** Clara manages long-running tasks that can wait for human
  input or external events without keeping a "hot" LLM context alive.

## Architecture at a Glance

Clara is built on three core pillars:

1. **The Registry (MCP-First):** Clara is an aggregator for
   [Model Context Protocol](https://modelcontextprotocol.io) servers. If a
   capability exists (Filesystem, Chrome, Slack, Photos), it is delivered
   through an MCP server. This keeps the core daemon focused on orchestration
   and policy.
2. **The Intents (Starlark):** High-level workflows are authored in Starlark
   (`.star`). These are declarative, Python-like scripts that define how tools
   should be used, when they should trigger, and how they should handle state.
3. **The Daemon:** A Go-based background service that manages MCP server
   lifecycles, watches your tasks directory, executes intents, and persists
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

Create a file in `~/.local/share/clara/tasks/hello.star`:

```python
# hello.star
clara.describe("A simple directory listing")

def main():
    # Call any tool from the unified MCP registry
    files = fs.list_directory(path = ".")
    return {"found": len(files)}
```

Run it immediately:

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
- **Worker:** Fixed-interval loops (e.g., `10m` for a file sync).
- Event-Driven: Reactive to MCP notifications (e.g.,
  `clara.on(macos.theme_on_change)`).


### The `wait` Pattern

One of Clara's most powerful features is the ability to pause execution for
human intervention:

```python
def main():
    # ... do some research ...
    approval = clara.wait("approval", prompt = "Should I send this email?")
    if approval.get("approved"):
        # ... proceed ...
```

Clara persists the state to disk and exits. When you approve the task in the
TUI, Clara reloads the state and resumes exactly where it left off.

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

You can use any MCP server with Clara, but I wasn't happy with the state of a
lot of the MCP projects out there. The built in ones are all written in Go
(except where another language was mandated), tested and maintained as a unit
with the rest of the solution, and they're simple, fast, lightweight, and
reliable.

### Setting up the Chrome Extension

The Chrome extension connects Clara to Chrome via Native Messaging. It
self-heals after browser restarts, sleeps, or server restarts, and auto-updates
whenever the server has a newer version.

**First-time setup (one-time):**

```bash
# 1. Write the latest extension files to disk
clara chrome update-extension
#    → ~/.local/share/clara/extension/

# 2. Load the extension in Chrome
#    Open: chrome://extensions
#    Enable "Developer mode" (toggle, top-right)
#    Click "Load unpacked" → select ~/.local/share/clara/extension/
#    Copy the Extension ID shown on that page

# 3. Register the Native Messaging host
clara chrome setup-native <EXTENSION_ID>

# 4. Quit and relaunch Chrome
#    The extension icon turns green when Clara is running.
```

**After a `clara` binary update:** no manual steps needed. When the extension
reconnects it sends its version; if it's out of date the server writes fresh
files to disk and sends a reload signal automatically.

**Icon states:**

| Icon | Meaning |
|------|---------|
| 🟢 Green circle | Connected — Clara is reachable |
| ⚫ Grey circle | Disconnected — Clara is not running or Native Messaging is not configured |

## Project Structure

- `cmd/clara/`: The unified binary (CLI + Daemon).
- `internal/interpreter/`: The Starlark runtime and state machine.
- `internal/mcpserver/`: Built-in MCP implementations.
- `swift/`: Native macOS bridge.
- `extension/`: Chrome extension source.

---

_Clara is under active development._
