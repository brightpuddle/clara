# TUI Scrolling Behavior Design

This specification defines the behavior for scrolling in the Clara TUI (HUD). The goal is to ensure that the most recent notifications are visible by default, while allowing the user to scroll up to read history without being interrupted by new updates.

## Current Behavior
- TUI starts with the scrollbar at the top (oldest notification).
- New notifications are appended to the end of the content but do not cause the view to scroll.
- The user must manually scroll down to see new content.

## Target Behavior
- **Initial Startup**: The TUI should automatically scroll to the very bottom to show the most recent notification.
- **History Load**: After history is loaded from the daemon, the TUI should scroll to the bottom of the history.
- **New Notifications**:
    - If the user is already at the bottom of the content area, new notifications should cause the view to scroll down, keeping the new notification visible.
    - If the user has scrolled up to read older content, new notifications should not cause the view to scroll, allowing uninterrupted reading.
- **Manual Scrolling**:
    - Scrolling up disables the "sticky bottom" behavior.
    - Scrolling to the very bottom re-enables the "sticky bottom" behavior.

## Technical Approach

### State Management
Modify `internal/tui/content.go`:
- Add `stickyBottom bool` to `contentModel`.
- Initialize `stickyBottom` to `true`.

### Rendering Logic
In `contentModel.View()`:
1. Calculate the total number of lines in the rendered content.
2. Calculate `maxScroll` (total lines - viewport height).
3. If `stickyBottom` is `true`:
   - Set `scrollOffset = maxScroll`.
4. If `stickyBottom` is `false`:
   - Clip `scrollOffset` to `maxScroll` (to handle window resizing or item removal).

### Input Handling
In `contentModel.Update()`:
1. **Move Up Actions** (`tea.MouseWheelUp`, `up`, `k`, `ctrl+u`):
   - Set `stickyBottom = false`.
2. **Move Down Actions** (`tea.MouseWheelDown`, `down`, `j`, `ctrl+d`):
   - Check if the resulting `scrollOffset` is greater than or equal to the current `maxScroll`.
   - If at the bottom, set `stickyBottom = true`.

## Testing Strategy
- **Unit Tests**:
  - Verify `stickyBottom` is true by default.
  - Verify scrolling up sets `stickyBottom` to false.
  - Verify scrolling to the bottom sets `stickyBottom` to true.
  - Verify `View` updates `scrollOffset` when `stickyBottom` is true and content grows.
- **Integration Tests**:
  - Simulate history load and verify scroll position is at the bottom.
  - Simulate a new notification while at the bottom and verify scroll position updates.
  - Simulate a new notification while scrolled up and verify scroll position remains stable.
