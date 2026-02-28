package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- Styles -----------------------------------------------------------------
// All colors use the ANSI 16-color palette so they adapt to any terminal theme.

var (
	promptStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))                 // blue ❯
	modeStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))                 // dim [N]/[I]
	separatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))                 // dim rule
	echoStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))                 // dim > echo
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)      // red errors
	successStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))                 // green success
	tabDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))                 // dim candidates
	tabSelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Underline(true) // selected candidate

	// Today view styles
	greetStyle   = lipgloss.NewStyle().Bold(true)
	dateStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	numStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	hintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// ---- Screen mode ------------------------------------------------------------

type screenMode int

const (
	screenToday screenMode = iota
	screenChat
)

// ---- Today data -------------------------------------------------------------

type todayItem struct {
	title     string // short display line, e.g. "meeting-notes  →  [[Project Alpha]]"
	detail    string // secondary info, e.g. "85% match"
	expandText string // shown in chat viewport when item is selected
	prefill   string // input pre-fill, e.g. "/approve 42"
}

type todaySection struct {
	name  string
	items []todayItem
}

type todayLoadedMsg struct{ sections []todaySection }
type showTodayMsg struct{}

// ---- Chat history -----------------------------------------------------------

type entryKind int

const (
	kindEcho    entryKind = iota
	kindResult
	kindError
	kindSuccess
)

type chatEntry struct {
	kind    entryKind
	content string
}

// ---- Model ------------------------------------------------------------------

type Model struct {
	api           *APIClient
	vp            viewport.Model
	input         vimInput
	history       []chatEntry
	ready         bool
	width         int
	height        int
	tabIndex      int
	screen        screenMode
	todaySections []todaySection
	todayLoading  bool
}

func New(api *APIClient) Model {
	return Model{
		api:          api,
		input:        newVimInput(),
		tabIndex:     -1,
		screen:       screenToday,
		todayLoading: true,
	}
}

func (m Model) Init() tea.Cmd {
	return m.loadToday()
}

// ---- Layout -----------------------------------------------------------------

func (m Model) inputAreaHeight() int {
	n := strings.Count(m.input.value(), "\n") + 1
	if n < 1 {
		n = 1
	}
	return n
}

func (m Model) statusAreaHeight() int {
	if len(m.inputCandidates()) > 0 {
		return 1
	}
	return 0
}

func (m Model) viewportHeight() int {
	// Today mode: separator(1) + hint(1); Chat mode: separator(1) + input(n) + status(0|1)
	var bottomHeight int
	if m.screen == screenToday {
		bottomHeight = 2 // separator + hint line
	} else {
		bottomHeight = 1 + m.inputAreaHeight() + m.statusAreaHeight()
	}
	h := m.height - bottomHeight
	if h < 1 {
		h = 1
	}
	return h
}

// inputCandidates returns visible command names matching the current input prefix.
func (m Model) inputCandidates() []string {
	val := strings.TrimSpace(m.input.value())
	if !strings.HasPrefix(val, "/") {
		return nil
	}
	withoutSlash := val[1:]
	if strings.ContainsAny(withoutSlash, " \t") {
		return nil
	}
	return candidatesFor(withoutSlash)
}

// updateViewportContent refreshes the viewport based on current screen.
func (m *Model) updateViewportContent() {
	if m.screen == screenToday {
		m.vp.SetContent(m.todayView())
	} else {
		m.vp.SetContent(m.renderHistory())
	}
}

// flatTodayItems returns all today items in order across all sections.
func (m Model) flatTodayItems() []todayItem {
	var out []todayItem
	for _, s := range m.todaySections {
		out = append(out, s.items...)
	}
	return out
}

// ---- Data loading -----------------------------------------------------------

func (m Model) loadToday() tea.Cmd {
	return func() tea.Msg {
		suggestions, err := m.api.ListSuggestions(context.Background())
		if err != nil {
			// Return empty today on API error so startup is not blocked.
			return todayLoadedMsg{}
		}

		var items []todayItem
		for _, s := range suggestions {
			src := filepath.Base(s.SourcePath)
			src = strings.TrimSuffix(src, filepath.Ext(src))
			title := fmt.Sprintf("%s  →  [[%s]]", src, s.TargetTitle)
			detail := fmt.Sprintf("%.0f%% match", s.Similarity*100)

			var expand strings.Builder
			fmt.Fprintf(&expand, "%s  →  [[%s]]  %.0f%%\n", src, s.TargetTitle, s.Similarity*100)
			if s.Context != "" {
				fmt.Fprintf(&expand, "  %s\n", truncate(s.Context, 100))
			}
			fmt.Fprintf(&expand, "\n  /approve %-6d  or  /reject %d", s.ID, s.ID)

			items = append(items, todayItem{
				title:     title,
				detail:    detail,
				expandText: expand.String(),
				prefill:   fmt.Sprintf("/approve %d", s.ID),
			})
		}

		var sections []todaySection
		if len(items) > 0 {
			sections = append(sections, todaySection{name: "Backlink Suggestions", items: items})
		}
		return todayLoadedMsg{sections: sections}
	}
}

// ---- Update -----------------------------------------------------------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.ready {
			m.vp = viewport.New(m.width, m.viewportHeight())
			m.ready = true
			m.updateViewportContent()
		} else {
			m.vp.Width = m.width
			m.vp.Height = m.viewportHeight()
		}

	case todayLoadedMsg:
		m.todaySections = msg.sections
		m.todayLoading = false
		if m.ready && m.screen == screenToday {
			m.vp.SetContent(m.todayView())
			m.vp.GotoTop()
		}

	case showTodayMsg:
		m.screen = screenToday
		m.todayLoading = true
		if m.ready {
			m.vp.Height = m.viewportHeight()
			m.vp.SetContent(m.todayView())
			m.vp.GotoTop()
		}
		return m, m.loadToday()

	case inputResultMsg:
		kind := kindResult
		if msg.err != nil {
			kind = kindError
			m.history = append(m.history, chatEntry{kind, "Error: " + msg.err.Error()})
		} else {
			m.history = append(m.history, chatEntry{kind, msg.output})
		}
		m.vp.SetContent(m.renderHistory())
		m.vp.GotoBottom()

	case findReadyMsg:
		cmd = execFzf(msg.files)

	case tea.KeyMsg:
		if m.screen == screenToday {
			m, cmd = m.handleTodayKey(msg)
		} else if m.input.mode == modeNormal {
			m, cmd = m.handleNormalKey(msg)
		} else {
			m, cmd = m.handleInsertKey(msg)
		}
		if m.ready {
			m.vp.Height = m.viewportHeight()
		}
		return m, cmd
	}

	return m, cmd
}

// ---- Today key handler ------------------------------------------------------

func (m Model) handleTodayKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "esc", "q":
		m.screen = screenChat
		if m.ready {
			m.vp.Height = m.viewportHeight()
			m.vp.SetContent(m.renderHistory())
			m.vp.GotoBottom()
		}

	case "/":
		m.screen = screenChat
		m.input.enterInsertEnd()
		m.input.insertChar('/')
		m.tabIndex = -1
		if m.ready {
			m.vp.Height = m.viewportHeight()
			m.vp.SetContent(m.renderHistory())
			m.vp.GotoBottom()
		}

	case "i":
		m.screen = screenChat
		m.input.enterInsert()
		if m.ready {
			m.vp.Height = m.viewportHeight()
			m.vp.SetContent(m.renderHistory())
			m.vp.GotoBottom()
		}

	case "j", "down":
		m.vp.LineDown(1)
	case "k", "up":
		m.vp.LineUp(1)
	case "ctrl+d":
		m.vp.HalfViewDown()
	case "ctrl+u":
		m.vp.HalfViewUp()

	default:
		// Number key: 1–9 selects the corresponding item.
		if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			n := int(s[0] - '0')
			return m.selectTodayItem(n)
		}
	}
	return m, nil
}

// selectTodayItem switches to chat mode, shows the item, and pre-fills the input.
func (m Model) selectTodayItem(n int) (Model, tea.Cmd) {
	items := m.flatTodayItems()
	if n < 1 || n > len(items) {
		return m, nil
	}
	item := items[n-1]

	m.screen = screenChat
	m.history = append(m.history, chatEntry{kindResult, item.expandText})
	if m.ready {
		m.vp.Height = m.viewportHeight()
		m.vp.SetContent(m.renderHistory())
		m.vp.GotoBottom()
	}

	// Pre-fill the input prompt with the suggested action.
	m.input.clear()
	m.input.enterInsert()
	for _, r := range item.prefill {
		m.input.insertChar(r)
	}
	return m, nil
}

// ---- Chat key handlers ------------------------------------------------------

func (m Model) handleNormalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.vp.LineDown(1)
	case "k", "up":
		m.vp.LineUp(1)
	case "ctrl+d":
		m.vp.HalfViewDown()
	case "ctrl+u":
		m.vp.HalfViewUp()
	case "G":
		m.vp.GotoBottom()
	case "i":
		m.input.enterInsert()
	case "a":
		m.input.enterInsertAfter()
	case "A":
		m.input.enterInsertEnd()
	case "I":
		m.input.enterInsertBeginning()
	case "/":
		m.input.enterInsertEnd()
		m.input.insertChar('/')
		m.tabIndex = -1
	case "h", "left":
		m.input.moveCursorLeft()
	case "l", "right":
		m.input.moveCursorRight()
	case "w":
		m.input.moveWordForward()
	case "b":
		m.input.moveWordBackward()
	case "0":
		m.input.moveCursorBeginning()
	case "$":
		m.input.moveCursorEnd()
	case "x":
		m.input.deleteAtCursor()
	case "D":
		m.input.deleteToEnd()
	}
	return m, nil
}

func (m Model) handleInsertKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input.exitInsert()
		m.tabIndex = -1

	case "ctrl+c":
		return m, tea.Quit

	case "enter":
		line := strings.TrimSpace(m.input.value())
		if line == "" {
			return m, nil
		}
		m.history = append(m.history, chatEntry{kindEcho, line})
		m.input.clear()
		m.input.mode = modeNormal
		m.tabIndex = -1
		cmd := dispatch(line, m.api)
		m.vp.SetContent(m.renderHistory())
		m.vp.GotoBottom()
		return m, cmd

	case "tab":
		candidates := m.inputCandidates()
		if len(candidates) == 0 {
			return m, nil
		}
		if len(candidates) == 1 {
			m.setInputToCommand(candidates[0], true)
			m.tabIndex = -1
		} else {
			m.tabIndex = (m.tabIndex + 1) % len(candidates)
			m.setInputToCommand(candidates[m.tabIndex], false)
		}
		return m, nil

	case "alt+enter":
		m.input.insertChar('\n')
		m.tabIndex = -1

	case "backspace":
		m.input.backspace()
		m.tabIndex = -1

	case "delete":
		m.input.deleteAtCursor()
		m.tabIndex = -1

	case "left":
		m.input.moveCursorLeft()
	case "right":
		m.input.moveCursorRight()

	default:
		if len(msg.Runes) == 1 {
			m.input.insertChar(msg.Runes[0])
			m.tabIndex = -1
		}
	}
	return m, nil
}

func (m *Model) setInputToCommand(name string, trailingSpace bool) {
	m.input.clear()
	s := "/" + name
	if trailingSpace {
		s += " "
	}
	for _, r := range s {
		m.input.insertChar(r)
	}
}

// ---- Rendering --------------------------------------------------------------

func (m Model) todayView() string {
	var sb strings.Builder

	now := time.Now()
	var greet string
	switch h := now.Hour(); {
	case h >= 5 && h < 12:
		greet = "Good morning!"
	case h >= 12 && h < 17:
		greet = "Good afternoon!"
	case h >= 17 && h < 21:
		greet = "Good evening!"
	default:
		greet = "Good night!"
	}

	sb.WriteString("\n  ")
	sb.WriteString(greetStyle.Render(greet))
	sb.WriteString("\n  ")
	sb.WriteString(dateStyle.Render(now.Format("Monday, January 2")))
	sb.WriteString("\n")

	if m.todayLoading {
		sb.WriteString("\n  ")
		sb.WriteString(echoStyle.Render("Loading…"))
		return sb.String()
	}

	if len(m.todaySections) == 0 {
		sb.WriteString("\n  ")
		sb.WriteString(echoStyle.Render("Nothing needs your attention right now."))
		return sb.String()
	}

	num := 1
	for _, section := range m.todaySections {
		sb.WriteString("\n  ")
		sb.WriteString(sectionStyle.Render("── " + section.name))
		sb.WriteString("\n\n")
		for _, item := range section.items {
			sb.WriteString(fmt.Sprintf("  %s  %s\n",
				numStyle.Render(fmt.Sprintf("%d.", num)),
				item.title,
			))
			if item.detail != "" {
				sb.WriteString(fmt.Sprintf("      %s\n", echoStyle.Render(item.detail)))
			}
			sb.WriteString("\n")
			num++
		}
	}
	return sb.String()
}

func (m Model) renderHistory() string {
	var sb strings.Builder
	sb.WriteString(welcomeText())
	sb.WriteString("\n")
	for i, entry := range m.history {
		if i > 0 {
			sb.WriteString("\n")
		}
		switch entry.kind {
		case kindEcho:
			sb.WriteString(echoStyle.Render("> " + entry.content))
		case kindResult:
			sb.WriteString(entry.content)
		case kindError:
			sb.WriteString(errorStyle.Render(entry.content))
		case kindSuccess:
			sb.WriteString(successStyle.Render(entry.content))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) statusView() string {
	candidates := m.inputCandidates()
	if len(candidates) == 0 {
		return ""
	}
	parts := make([]string, len(candidates))
	for i, name := range candidates {
		label := "/" + name
		if i == m.tabIndex {
			parts[i] = tabSelStyle.Render(label)
		} else {
			parts[i] = tabDimStyle.Render(label)
		}
	}
	return "  " + strings.Join(parts, "  ")
}

func welcomeText() string {
	return echoStyle.Render("Type /help for available commands. Press i or / to begin.")
}

func (m Model) View() string {
	if !m.ready {
		return ""
	}

	sep := separatorStyle.Render(strings.Repeat("─", m.width))

	if m.screen == screenToday {
		hint := hintStyle.Render("  1-9 select  ·  esc chat  ·  / command")
		return strings.Join([]string{m.vp.View(), sep, hint}, "\n")
	}

	// Chat mode
	modeLabel := "[N]"
	if m.input.mode == modeInsert {
		modeLabel = "[I]"
	}
	inputLine := fmt.Sprintf("%s %s %s",
		modeStyle.Render(modeLabel),
		promptStyle.Render("❯"),
		m.input.view(),
	)
	parts := []string{m.vp.View(), sep, inputLine}
	if s := m.statusView(); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}

// ---- fzf integration --------------------------------------------------------

func execFzf(files []string) tea.Cmd {
	tmpIn, err := os.CreateTemp("", "clara-find-in-*")
	if err != nil {
		e := fmt.Errorf("create temp file: %w", err)
		return func() tea.Msg { return inputResultMsg{err: e} }
	}
	fmt.Fprint(tmpIn, strings.Join(files, "\n"))
	tmpIn.Close()

	tmpOut, err := os.CreateTemp("", "clara-find-out-*")
	if err != nil {
		os.Remove(tmpIn.Name())
		e := fmt.Errorf("create temp file: %w", err)
		return func() tea.Msg { return inputResultMsg{err: e} }
	}
	tmpOut.Close()

	shellCmd := fmt.Sprintf("fzf < %s > %s", shellEscape(tmpIn.Name()), shellEscape(tmpOut.Name()))
	cmd := exec.Command("sh", "-c", shellCmd)

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(tmpIn.Name())
		defer os.Remove(tmpOut.Name())

		selBytes, readErr := os.ReadFile(tmpOut.Name())
		path := strings.TrimSpace(string(selBytes))
		if readErr != nil || path == "" {
			return inputResultMsg{output: "Find cancelled."}
		}
		if openErr := exec.Command("open", path).Start(); openErr != nil {
			return inputResultMsg{err: fmt.Errorf("open %s: %w", path, openErr)}
		}
		return inputResultMsg{output: "Opened: " + path}
	})
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
