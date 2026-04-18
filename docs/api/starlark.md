# Starlark API Reference

This document provides a comprehensive reference for the built-in functions available in Clara's Starlark environment.

## Table of Contents
- [Namespace: must](#namespace-must)
  - [eq](#eq)
  - [neq](#neq)
  - [true](#true)
  - [false](#false)
  - [fails](#fails)
- [Namespace: clara](#namespace-clara)
  - [describe](#describe)
  - [task](#task)
  - [on](#on)
  - [wait](#wait)
  - [search](#search)
- [Namespace: tui](#namespace-tui)
  - [notify.send](#notifysend)
  - [notify.send_interactive](#notifysend_interactive)
- [Dynamic MCP Namespaces](#dynamic-mcp-namespaces)

---

## Namespace: must
The `must` module is used for runtime assertions in production scripts and for defining checks in unit tests (`*_test.star`).

### eq
`must.eq(x, y)`

Asserts that two values are equal.

**Parameters:**
- `x`: The first value to compare.
- `y`: The second value to compare.

**Example:**
```python
def test_math():
    must.eq(1 + 1, 2)
```

### neq
`must.neq(x, y)`

Asserts that two values are not equal.

**Parameters:**
- `x`: The first value.
- `y`: The second value.

**Example:**
```python
def test_inequality():
    must.neq(5, 3)
```

### true
`must.true(cond)`

Asserts that a condition is truthy.

**Parameters:**
- `cond`: The condition to evaluate.

**Example:**
```python
def main():
    files = fs.list_directory(path = ".")
    must.true(len(files) > 0)
```

### false
`must.false(cond)`

Asserts that a condition is falsy.

**Parameters:**
- `cond`: The condition to evaluate.

**Example:**
```python
def test_empty():
    items = []
    must.false(len(items) > 0)
```

### fails
`must.fails(fn)`

Asserts that calling the provided function results in a Starlark error.

**Parameters:**
- `fn`: A callable function (usually a lambda or a local def).

**Example:**
```python
def test_validation():
    def bad_call():
        clara.wait("", prompt="") # Missing name
    must.fails(bad_call)
```

---

## Namespace: clara
The `clara` module provides core orchestration primitives, task registration, and unified cross-tool capabilities.

### describe
`clara.describe(text)`

Sets a human-readable description for the intent. This is displayed in the TUI and CLI logs.

**Parameters:**
- `text`: A string describing what the intent does.

**Example:**
```python
clara.describe("Syncs Taskwarrior tasks with Apple Reminders")
```

### task
`clara.task(handler, mode="on_demand", interval=None, schedule=None, trigger=None)`

Registers a function as a managed task within the intent.

**Parameters:**
- `handler`: The function to execute.
- `mode`: The execution mode ("on_demand", "worker", "scheduled", or "reactive").
- `interval`: (For "worker") A duration string like "10m" or "1h".
- `schedule`: (For "scheduled") A cron-style string like "0 7 * * *".
- `trigger`: (For "reactive") An event reference created via `clara.on()`.

**Example:**
```python
def my_sync():
    print("Syncing...")

clara.task(my_sync, interval = "15m")
```

### on
`clara.on(event_ref)`

Wraps a tool reference to treat it as an event trigger.

**Parameters:**
- `event_ref`: A reference to a tool that supports notifications (e.g., `macos.on_change`).

**Example:**
```python
clara.task(handler, trigger = clara.on(macos.on_change))
```

### wait
`clara.wait(name, prompt, options=[])`

Pauses script execution and waits for human intervention. The script's state is persisted, and it resumes once the user provides input via the TUI.

**Parameters:**
- `name`: A unique identifier for this wait point (used for replay).
- `prompt`: The message to display to the user.
- `options`: (Optional) A list of strings for multiple-choice selection.

**Returns:**
- `any`: The value provided by the user.

**Example:**
```python
def main():
    res = clara.wait("approval", prompt = "Should I proceed?", options = ["Yes", "No"])
    if res == "Yes":
        print("Proceeding...")
```

### search
`clara.search(query, limit=10)`

Performs a unified search across all MCP servers that implement search capabilities (e.g., Mail, Zettelkasten, Webex).

**Parameters:**
- `query`: The search string.
- `limit`: (Optional) Maximum results to return per source.

**Returns:**
- `dict`: A dictionary of results keyed by source name.

**Example:**
```python
def main():
    results = clara.search(query = "Project Clara")
    print(results.get("zk")) # Access Zettelkasten results
```

---

## Namespace: tui
The `tui` module handles direct interaction with the Clara Terminal User Interface.

### notify.send
`tui.notify.send(message)`

Sends a non-blocking notification to the TUI HUD.

**Parameters:**
- `message`: The text to display.

**Example:**
```python
tui.notify.send("Backup completed successfully")
```

### notify.send_interactive
`tui.notify.send_interactive(prompt, options=[])`

Sends a prompt to the TUI and blocks execution until the user responds.

**Parameters:**
- `prompt`: The question to ask.
- `options`: (Optional) A list of selection choices.

**Example:**
```python
def main():
    choice = tui.notify.send_interactive("Select environment", options=["dev", "prod"])
    print("Selected: " + choice)
```

---

## Dynamic MCP Namespaces

Every MCP server connected to Clara is automatically exposed as a top-level namespace.

- **Convention:** `namespace.tool_name(args)`
- **Mapping:** Starlark underscores (`_`) are automatically converted to MCP hyphens (`-`).
- **Example:** `fs.read_file(path="...")` calls the `fs.read-file` tool.

**Common Dynamic Namespaces:**
- `fs`: Filesystem operations (`read_file`, `write_file`, `list_directory`).
- `chrome`: Browser automation (`navigate`, `click`, `fill`).
- `llm`: Language model generation (`generate`, `embed`).
- `github`: Repository management (`list_issues`, `create_pull_request`).
- `macos`: Native system integration (`list_reminders`, `create_calendar_event`).
