---
plan_recommended: true
---

# TUI Complete Rebuild (OpenCode-Inspired Design)

## Planning Context

This is a complete throwaway and rebuild of `internal/tui/`. Nothing from the current design is
retained. The target aesthetic and UX is OpenCode's terminal UI. Planning should address:

- **Detailed visual analysis of OpenCode**
- **Bubbletea component mapping**
- **Two-panel split**
- **Sidebar behavior**
- **Q&A / suggestion model**
- **IPC integration**
- **Testing strategy**

## Context

The current TUI is a prompt-with-slash-commands design. The new TUI follows the Clara vision: a
HUD where I can quickly see what needs my attention, receive AI-generated suggestions, respond to
Q&A, and ask questions about my data. OpenCode is the closest existing design to this vision.

Clara is early; backwards compatibility is not a concern. Delete everything in `internal/tui/`
and start fresh.

## Target Layout

```text
┌──────────────────────────────────────────────┬──────────────────┐
│                                              │                  │
│  Content Area (scrollable)                   │  Sidebar         │
│                                              │  (>120 cols)     │
│  • Prioritized attention items               │                  │
│  • AI responses                              │  Intent status   │
│  • Q&A suggestions (numbered)               │  Top priorities  │
│  • Tool call results                         │  Context         │
│  • Intent run logs (collapsible)             │                  │
│                                              │                  │
├──────────────────────────────────────────────┴──────────────────┤
│  Prompt / Input area                                             │
│  > Type a question or pick an option (1-9)...                   │
└──────────────────────────────────────────────────────────────────┘
```

## Visual Style (from OpenCode)

- **Two-tone shading**
- **Left border highlight**
- **Block ASCII scrollbars**
- **Modals**
- **Collapsible sections**
- **Status bar**
- **Color palette**
- **No separate splash screen**

## Interaction Model

### Content Area
- Scrollable with keyboard and mouse
- Typed items: `attention_item`, `ai_response`, `qa_suggestion`, `tool_result`, `intent_log`
- `qa_suggestion` items show numbered options (1-9)

### Prompt Area
- Multi-line input
- Typing a number (1-9) when a Q&A item is active selects that option
- `/` prefix opens the command palette
- Up/Down history, Tab autocomplete, Ctrl+L clear, Ctrl+X open editor

### Sidebar (when viewport >= 120 columns)
- Shows top 3-5 priority items
- Active intent run status
- Context items

## IPC Integration

The TUI attaches to the daemon as a dynamic MCP peer, exposing `notify_send` and
`notify_send_interactive` tools. Extend the IPC client to also support:
- Streaming content area updates
- Q&A interaction protocol

## Acceptance Criteria

- The TUI compiles and runs; `clara` opens the new TUI
- Content area, prompt area, and sidebar render correctly at different terminal widths
- Left highlight, two-tone shading, block scrollbars, and collapsible items are implemented
- Numbered option selection (1-9) works for Q&A items
- At least basic IPC-backed operations work: intent list, tool call, notification receipt
- The `notify_send_interactive` tool triggers a Q&A item in the content area
- Tests cover model state transitions, rendering, IPC parsing
