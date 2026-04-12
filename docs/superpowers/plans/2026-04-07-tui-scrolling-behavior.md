# TUI Scrolling Behavior Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement auto-scrolling to the bottom of notifications in the TUI, while allowing manual scrolling to override this "stickiness".

**Architecture:** Add a `stickyBottom` flag to `contentModel`. Use this flag in `View` to force `scrollOffset` to `maxScroll`. Update the flag in `Update` based on user scroll actions.

**Tech Stack:** Go, Bubbletea, Lipgloss

---

### Task 1: Setup Scrolling Tests

**Files:**
- Create: `internal/tui/scrolling_test.go`

- [ ] **Step 1: Create initial test file with a failing test for startup scroll position**

```go
package tui

import (
	"testing"
	"strings"
)

func TestStickyBottom_InitialScroll(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 5) // Height 5

	// Add enough items to exceed viewport
	for i := 0; i < 10; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line " + strings.Repeat("x", 10)})
	}

	// First View() should set scrollOffset to bottom if stickyBottom is true
	m.View()

	if m.scrollOffset == 0 {
		t.Errorf("expected scrollOffset to be at bottom, got 0")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v internal/tui/scrolling_test.go internal/tui/content.go internal/tui/theme.go internal/tui/app.go` (or just `go test -v ./internal/tui`)
Expected: FAIL (scrollOffset will be 0)

- [ ] **Step 3: Commit**

```bash
git add internal/tui/scrolling_test.go
git commit -m "test: add failing test for initial scroll position"
```

---

### Task 2: Implement Initial Sticky Bottom

**Files:**
- Modify: `internal/tui/content.go`

- [ ] **Step 1: Add `stickyBottom` field and initialize it**

```go
// internal/tui/content.go

type contentModel struct {
	// ... existing fields
	stickyBottom  bool // Add this
}

func newContentModel(theme *Theme) *contentModel {
	return &contentModel{
		theme: theme,
		items: []ContentItem{
			{Type: "notification", Text: "System online. Waiting for intents..."},
		},
		selectedIndex: -1,
		stickyBottom:  true, // Initialize to true
	}
}
```

- [ ] **Step 2: Implement sticky scroll logic in `View`**

```go
// internal/tui/content.go

func (m *contentModel) View() string {
	// ... (after calculating maxScroll)

	if m.stickyBottom {
		m.scrollOffset = maxScroll
	} else if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}

	// ... (rest of View)
}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test -v ./internal/tui`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tui/content.go
git commit -m "feat: implement initial sticky bottom behavior"
```

---

### Task 3: Handle Scrolling Up to Disable Stickiness

**Files:**
- Modify: `internal/tui/scrolling_test.go`
- Modify: `internal/tui/content.go`

- [ ] **Step 1: Add test for disabling stickiness on scroll up**

```go
// internal/tui/scrolling_test.go

func TestStickyBottom_ScrollUpDisables(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 5)
	for i := 0; i < 10; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line"})
	}
	m.View() // Initialize scrollOffset to bottom

	// Simulate scroll up
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp})

	if m.stickyBottom {
		t.Errorf("expected stickyBottom to be false after scrolling up")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/tui`
Expected: FAIL

- [ ] **Step 3: Update `Update` logic for all "Up" actions**

```go
// internal/tui/content.go

func (m *contentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			if m.scrollOffset > 0 {
				m.scrollOffset--
				m.stickyBottom = false // Add this
			}
		// ...
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedIndex > 0 {
				m.selectedIndex--
			} else if m.scrollOffset > 0 {
				m.scrollOffset--
				m.stickyBottom = false // Add this
			}
		case "ctrl+u":
			m.scrollOffset -= m.height / 2
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			m.stickyBottom = false // Add this
		// ...
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/tui`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/scrolling_test.go internal/tui/content.go
git commit -m "feat: scrolling up disables sticky bottom"
```

---

### Task 4: Handle Scrolling Down to Re-enable Stickiness

**Files:**
- Modify: `internal/tui/scrolling_test.go`
- Modify: `internal/tui/content.go`

- [ ] **Step 1: Add test for re-enabling stickiness on scroll to bottom**

```go
// internal/tui/scrolling_test.go

func TestStickyBottom_ScrollDownReenables(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 10)
	for i := 0; i < 100; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line"})
	}
	m.View() // Force scroll to bottom (stickyBottom=true)
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp}) // stickyBottom=false

	// Manual scroll down to bottom
	// We need to know maxScroll. View() calculates it but it's internal.
	// We'll just scroll a lot.
	for i := 0; i < 500; i++ {
		m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	}

	if !m.stickyBottom {
		t.Errorf("expected stickyBottom to be true after scrolling to bottom")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/tui`
Expected: FAIL

- [ ] **Step 3: Update `Update` logic for all "Down" actions**

Note: To check if we are at the bottom, we need to know the line count. Since line count is calculated in `View`, we might need a small helper or just do it in `Update` if we can estimate, but `View` is where the truth is.
Actually, if `Update` sets a value that `View` then clips and finds it's at the bottom, we can re-enable it.
Or better: In `Update`, if scrolling down, we just set a very high value or increment. `View` will clip it.
We can check if `m.scrollOffset == maxScroll` in `View` and if so set `stickyBottom = true`.

Wait, the requirement says: "If the scroll bar is at the bottom, new notifications should continue to scroll the content area to the bottom".

Let's refine the logic in `View`:
```go
	maxScroll := len(lines) - m.height
	if maxScroll < 0 {
		maxScroll = 0
	}

    // If user manually scrolled to the bottom, re-enable stickiness
    if !m.stickyBottom && m.scrollOffset >= maxScroll {
        m.stickyBottom = true
    }

	if m.stickyBottom {
		m.scrollOffset = maxScroll
	} else if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}
```

- [ ] **Step 4: Implement re-enable logic in `View`**

```go
// internal/tui/content.go

func (m *contentModel) View() string {
	// ... (after calculating maxScroll)

	// If we were not sticky but we reached the bottom (via manual scroll), re-enable it
	if !m.stickyBottom && m.scrollOffset >= maxScroll {
		m.stickyBottom = true
	}

	if m.stickyBottom {
		m.scrollOffset = maxScroll
	} else if m.scrollOffset > maxScroll {
		m.scrollOffset = maxScroll
	}
	// ...
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -v ./internal/tui`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/scrolling_test.go internal/tui/content.go
git commit -m "feat: scrolling to bottom re-enables sticky bottom"
```

---

### Task 5: Verify Auto-scroll on New Notifications

**Files:**
- Modify: `internal/tui/scrolling_test.go`

- [ ] **Step 1: Add test for auto-scrolling when sticky**

```go
// internal/tui/scrolling_test.go

func TestStickyBottom_AutoScrollOnNewItem(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 5)
	for i := 0; i < 10; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line"})
	}
	m.View()
	initialScroll := m.scrollOffset

	// Add another item
	m.addItem(ContentItem{Type: "notification", Text: "New Line"})
	m.View()

	if m.scrollOffset <= initialScroll {
		t.Errorf("expected scrollOffset to increase after adding item, got %d -> %d", initialScroll, m.scrollOffset)
	}
}

func TestStickyBottom_NoAutoScrollWhenNotSticky(t *testing.T) {
	theme := DefaultTheme()
	m := newContentModel(theme)
	m.SetSize(100, 5)
	for i := 0; i < 10; i++ {
		m.addItem(ContentItem{Type: "notification", Text: "Line"})
	}
	m.View()
	m.Update(tea.MouseMsg{Type: tea.MouseWheelUp}) // Disable sticky
	m.View()
	initialScroll := m.scrollOffset

	// Add another item
	m.addItem(ContentItem{Type: "notification", Text: "New Line"})
	m.View()

	if m.scrollOffset != initialScroll {
		t.Errorf("expected scrollOffset to remain stable when not sticky, got %d -> %d", initialScroll, m.scrollOffset)
	}
}
```

- [ ] **Step 2: Run all tests**

Run: `go test -v ./internal/tui`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/tui/scrolling_test.go
git commit -m "test: verify auto-scrolling and manual override"
```
