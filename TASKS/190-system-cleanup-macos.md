---
plan_recommended: false
---

# macOS System Cleanup Intent

## Context

My macOS system has accumulated significant cruft over years of use: app data for apps I no longer
have installed, orphaned configuration files in `~/Library`, unused Homebrew formulae and taps,
and dotfiles from long-gone tools.

**Dependencies:**
- FS MCP server
- LLM MCP server

## What to Clean

### 1. `~/Library/Application Support/`
Identify app support directories for apps that are no longer installed.

### 2. `~/Library/Preferences/`
Identify orphaned preference files by bundle ID.

### 3. `~/Library/Caches/`
Identify stale caches, especially large ones and caches for uninstalled apps.

### 4. Dot Files in `~`
Identify hidden files/directories that belong to tools no longer installed.

### 5. Homebrew Cleanup
Find unused, outdated, or problematic packages and taps.

### 6. `~/.config/`
Identify orphaned config directories for removed tools.

## TUI Interaction

Each cleanup candidate is presented with size, explanation, and options like:
- Move to Trash
- Open in Finder
- Keep
- Skip similar items

## Acceptance Criteria

- Orphaned `~/Library/Application Support/` directories are identified
- Homebrew formulae not used for 6+ months are flagged
- Each candidate is presented in the TUI with clear actions
- "Move to Trash" is reversible
- All decisions are logged
- Batch approval works for similar items
