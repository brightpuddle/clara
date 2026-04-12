# TUI Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Simplify the Clara TUI by removing the sidebar, eliminating artificial background highlights, and standardizing on a two-panel vertical layout with a "❯ " prompt.

**Architecture:** 
- Refactor `Theme` to use strictly ANSI colors (0-15) and remove dynamic background shading.
- Remove `sidebarModel` and all associated logic from `appModel`.
- Update `promptModel` for correct cursor alignment and the new "❯ " prompt character.
- Standardize `appModel` to a vertical split between `content` (top) and `prompt` (bottom).

**Tech Stack:** Go, Bubble Tea, Lipgloss.

---

### Task 1: Simplify Theme (ANSI Only)

**Files:**
- Modify: `internal/tui/theme.go`

- [ ] **Step 1: Remove dynamic color calculation and hex fallbacks**
Replace `DefaultTheme` and remove `calculateDynamicColors` and `clamp`.

```go
func DefaultTheme() *Theme {
	t := &Theme{
		Primary:   lipgloss.Color("12"), // ANSI Blue
		Secondary: lipgloss.Color("6"),  // ANSI Cyan
		Highlight: lipgloss.Color("13"), // ANSI Magenta
		Text:      lipgloss.Color("7"),  // ANSI White
		Dim:       lipgloss.Color("8"),  // ANSI Gray
		Error:     lipgloss.Color("9"),  // ANSI Red
		Success:   lipgloss.Color("10"), // ANSI Green
	}

	t.DimStyle = lipgloss.NewStyle().Foreground(t.Dim)
	t.BaseStyle = lipgloss.NewStyle().Foreground(t.Text)

	// Remove SidebarStyle entirely later, but for now simplify it
	t.SidebarStyle = lipgloss.NewStyle().
		Padding(1, 2).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Dim)

	t.PromptStyle = lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Dim).
		Padding(0, 1)

	t.ActiveItem = lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(t.Highlight).
		Padding(0, 1)

	t.InactiveItem = lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Dim).
		Padding(0, 1)

	t.TitleStyle = lipgloss.NewStyle().
		Foreground(t.Secondary).
		Bold(true).
		PaddingBottom(1)

	return t
}
```

- [ ] **Step 2: Clean up unused fields in Theme struct**
Remove `SidebarBg` and `PlaceholderColor`.

- [ ] **Step 3: Run TUI tests to ensure no breakage**
Run: `go test ./internal/tui/...`

- [ ] **Step 4: Commit**
```bash
git add internal/tui/theme.go
git commit -m "style: simplify theme to use ANSI colors only"
```

---

### Task 2: Remove Sidebar from App

**Files:**
- Modify: `internal/tui/app.go`
- Delete: `internal/tui/sidebar.go`

- [ ] **Step 1: Remove sidebar field and initialization in `appModel`**
Modify `Run` and `appModel` struct.

- [ ] **Step 2: Simplify `Update` logic for resizing**
Remove `showSidebar` logic.

```go
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height

    promptHeight := 3 // Border + 1 line input + padding
    contentHeight := m.height - promptHeight

    m.prompt.SetSize(m.width, promptHeight)
    m.content.SetSize(m.width, contentHeight)
```

- [ ] **Step 3: Simplify `View` logic**
Remove `lipgloss.JoinHorizontal` and just return the vertical join.

```go
func (m *appModel) View() string {
	if m.width == 0 {
		return "Initializing HUD..."
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.content.View(),
		m.prompt.View(),
	)
}
```

- [ ] **Step 4: Delete `sidebar.go`**
Run: `rm internal/tui/sidebar.go`

- [ ] **Step 5: Commit**
```bash
git add internal/tui/app.go
git rm internal/tui/sidebar.go
git commit -m "refactor: remove sidebar from TUI"
```

---

### Task 3: Standardize Prompt and Cursor

**Files:**
- Modify: `internal/tui/prompt.go`

- [ ] **Step 1: Update prompt character to "❯ "**
Ensure the trailing space is included.

```go
func newPromptModel(theme *Theme) *promptModel {
	ta := textarea.New()
	ta.Placeholder = "Type a question or pick an option (1-9)..."
	ta.Focus()

	ta.Prompt = "❯ "
    // ...
```

- [ ] **Step 2: Remove background styling from textarea**
Update `FocusedStyle` and `BlurredStyle` to not use `theme.SidebarBg`.

- [ ] **Step 3: Adjust `SetSize` to account for new vertical layout**
```go
func (m *promptModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Remove horizontal overhead calculation if using full width
	m.input.SetWidth(m.width - 4) // "❯ " + padding
	m.input.SetHeight(1)
}
```

- [ ] **Step 4: Commit**
```bash
git add internal/tui/prompt.go
git commit -m "feat: update prompt character and fix cursor alignment"
```

---

### Task 4: Final Verification

- [ ] **Step 1: Build and run the TUI**
Run: `go build ./cmd/clara && ./clara hud` (assuming hud is the command, or just test Run)

- [ ] **Step 2: Verify visual layout**
Check that the prompt is at the bottom, full width, and cursor is correctly positioned.

- [ ] **Step 3: Verify ANSI color usage**
Ensure no hardcoded hex backgrounds are visible.
