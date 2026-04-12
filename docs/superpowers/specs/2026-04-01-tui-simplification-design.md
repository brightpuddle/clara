# TUI Simplification Design Spec

**Date:** 2026-04-01  
**Status:** Approved  
**Topic:** TUI Simplification & UI/UX Cleanup

## Overview
The goal is to simplify the Clara TUI by removing unnecessary complexity, specifically the right-hand sidebar and artificial background shading. We aim for a "wireframe-first" approach that prioritizes utility and follows standard agentic development tool patterns.

## 1. Layout Architecture
The TUI will be refactored into a standardized two-panel vertical layout.

### Changes:
- **Remove Sidebar:** The `sidebarModel` and all associated logic in `app.go` will be deleted.
- **Two-Panel Model:**
    - **Communication Area (Top):** Occupies the full width and the majority of the height. Scrollable history.
    - **Prompt Area (Bottom):** Fixed at the bottom. Full width, single line (with padding).
- **Resizing Logic:** `appModel.Update` will handle `WindowSizeMsg` by simply splitting the terminal width and height between these two panels, removing any sidebar-width calculations.

## 2. Theme & Styling
We will strip the TUI of complex, dynamic color calculations and hard-coded fallbacks.

### Changes:
- **Remove Dynamic Colors:** Remove `CalculateDynamicColors` and associated logic in `theme.go`.
- **ANSI-Only Policy:** The TUI will exclusively use ANSI colors (0-15). No more hex fallbacks like `#2e343f`.
- **Remove Background Highlights:**
    - Secondary background colors (like `SidebarBg`) will be removed.
    - All panels will use the terminal's default background.
    - Styles like `PromptStyle` and `SidebarStyle` (to be removed/renamed) will no longer apply background fills.
- **Simplified Borders:** Borders will use basic ANSI colors (e.g., Magenta/ANSI 5 for the prompt border, Dim/ANSI 8 for separators).

## 3. Prompt Interaction
The prompt will be updated to match modern CLI expectations.

### Changes:
- **Prompt Character:** Use `❯ ` (including the trailing space) as the prompt.
- **Prompt Color:** The prompt foreground will retain its current color (Magenta / ANSI 5).
- **Cursor Alignment:** The cursor will sit one character to the right of the prompt character, separated by a single space.
- **Textarea Configuration:** Update `promptModel` to use the new prompt string and ensure consistent styling across focused/blurred states.

## 4. Code Cleanup & Robustness
- **Delete File:** `internal/tui/sidebar.go`.
- **Cleanup Imports:** Remove sidebar references from `internal/tui/app.go`.
- **Refactor `appModel`:** Remove `sidebar` field and `showSidebar` logic.
- **Consolidate Styles:** Review `theme.go` to ensure all styles are minimal and use the 16-color ANSI palette.

## 5. Testing & Verification
- **Visual Check:** Verify the TUI renders correctly at various terminal sizes.
- **Input Check:** Ensure the prompt remains focused and the cursor is correctly positioned.
- **No Regressions:** Verify that communication history and interactive Q&A still function as expected.
