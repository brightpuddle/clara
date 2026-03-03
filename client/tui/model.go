package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- Styles -----------------------------------------------------------------
// All colors use the ANSI 16-color palette so they adapt to any terminal theme.

var (
	promptStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))                 // blue ❯/❮
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
	title      string       // short display line, e.g. "meeting-notes  →  [[Project Alpha]]"
	detail     string       // secondary info, e.g. "85% match"
	expandText string       // shown in chat viewport when item is selected
	prefill    string       // input pre-fill, e.g. "/approve 42"
	proposal   *ClaraItemJSON // underlying proposal (nil for non-proposal items)
}

type todaySection struct {
	name  string
	items []todayItem
}

type todayLoadedMsg struct{ sections []todaySection }
type showTodayMsg struct{}
type noteSelectedMsg struct{ path string } // fzf selected a note; open in editor

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

func (m Model) viewportHeight() int {
	// Today mode: separator(1) + hint(1); Chat mode: separator(1) + input(n) + status(1, always reserved)
	var bottomHeight int
	if m.screen == screenToday {
		bottomHeight = 2 // separator + hint line
	} else {
		bottomHeight = 1 + m.inputAreaHeight() + 1 // separator + input + status (always 1 line)
	}
	h := m.height - bottomHeight
	if h < 1 {
		h = 1
	}
	return h
}

// inputCompletionBase returns the fixed prefix to preserve when tab-completing.
// For top-level completion it is ""; for subcommand completion it is "cmd ".
func (m Model) inputCompletionBase() string {
	val := strings.TrimLeft(m.input.value(), " \t")
	val = strings.TrimPrefix(val, "/") // tolerate optional leading slash
	idx := strings.IndexAny(val, " \t")
	if idx < 0 {
		return ""
	}
	return val[:idx] + " "
}

// inputCandidates returns completable names matching the current input prefix.
// For top-level it returns command names; for subcommands it returns sub-names.
func (m Model) inputCandidates() []string {
	val := strings.TrimLeft(m.input.value(), " \t")
	val = strings.TrimPrefix(val, "/") // tolerate optional leading slash
	if val == "" {
		return nil
	}
	idx := strings.IndexAny(val, " \t")
	if idx < 0 {
		// Top-level: no space yet.
		return candidatesFor(val)
	}
	// Subcommand: text after the first space (must be a single word).
	cmdName := val[:idx]
	rest := strings.TrimLeft(val[idx+1:], " \t")
	if strings.ContainsAny(rest, " \t") {
		return nil // second space — nothing to complete
	}
	return subCandidatesFor(cmdName, rest)
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
		proposals, err := m.api.GetProposals(context.Background())
		if err != nil {
			// Return empty today on API error so startup is not blocked.
			return todayLoadedMsg{}
		}

		var items []todayItem
		for i := range proposals {
			p := &proposals[i]
			title, detail := proposalSummary(p)
			expandText := proposalExpandText(p)
			// Extract numeric ID from "suggestion-<n>"
			prefill := fmt.Sprintf("/approve %s", p.ID)
			if numID := extractNumericID(p.ID); numID != "" {
				prefill = fmt.Sprintf("/approve %s", numID)
			}
			items = append(items, todayItem{
				title:      title,
				detail:     detail,
				expandText: expandText,
				prefill:    prefill,
				proposal:   p,
			})
		}

		var sections []todaySection
		if len(items) > 0 {
			sections = append(sections, todaySection{name: "Backlink Suggestions", items: items})
		}
		return todayLoadedMsg{sections: sections}
	}
}

// proposalSummary returns the one-line title and detail for the today list.
func proposalSummary(p *ClaraItemJSON) (title, detail string) {
	if p.Body == "" {
		return p.ID, ""
	}
	// First non-empty line of the body is the title.
	lines := strings.SplitN(p.Body, "\n", 2)
	title = strings.TrimSpace(lines[0])
	if len(lines) > 1 {
		// Find the similarity line for detail.
		for _, line := range strings.Split(lines[1], "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Similarity:") {
				detail = line
				break
			}
		}
	}
	return title, detail
}

// proposalExpandText renders the proposal as YAML frontmatter + markdown for
// display in the chat viewport when the user selects it.
func proposalExpandText(p *ClaraItemJSON) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("id:             %s\n", p.ID))
	sb.WriteString(fmt.Sprintf("type:           %s\n", p.Type))
	sb.WriteString(fmt.Sprintf("source:         %s\n", p.Source))
	if p.SourceRef != "" {
		sb.WriteString(fmt.Sprintf("source_ref:     %s\n", p.SourceRef))
	}
	sb.WriteString(fmt.Sprintf("status:         %s\n", p.Status))
	sb.WriteString(fmt.Sprintf("action_surface: %s\n", p.ActionSurface))
	sb.WriteString("---\n")
	if p.Body != "" {
		sb.WriteString(p.Body)
		if !strings.HasSuffix(p.Body, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// extractNumericID extracts the numeric part from "suggestion-42" → "42".
func extractNumericID(id string) string {
	parts := strings.SplitN(id, "-", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return id
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

	case notesReadyMsg:
		if msg.newFile != "" {
			cmd = execEditor(msg.newFile)
		} else {
			cmd = execFzfNotes(msg.files)
		}

	case noteSelectedMsg:
		cmd = execEditor(msg.path)

	case remindersReadyMsg:
		cmd = execFzfReminders(msg.items, msg.editMode)

	case reminderSelectedMsg:
		m.screen = screenChat
		text := reminderToClaraItem(msg.item)
		m.history = append(m.history, chatEntry{kindResult, text})
		m.vp.SetContent(m.renderHistory())
		m.vp.GotoBottom()

	case reminderEditSelectedMsg:
		cmd = execReminderEditor(msg.item)

	case editDoneMsg:
		if msg.err != nil {
			m.history = append(m.history, chatEntry{kindError, "Edit error: " + msg.err.Error()})
		} else if msg.original != nil && len(msg.content) > 0 {
			// Parse status from edited YAML frontmatter; apply approve/dismiss accordingly.
			edited := parseEditedStatus(msg.content)
			var result string
			var apiErr error
			switch edited {
			case "approved":
				if numID := extractNumericID(msg.original.ID); numID != "" {
					result, apiErr = runApproveByID(numID, m.api)
				}
			case "dismissed":
				if numID := extractNumericID(msg.original.ID); numID != "" {
					if n, convErr := strconv.ParseInt(numID, 10, 64); convErr == nil {
						apiErr = m.api.Dismiss(context.Background(), n)
						if apiErr == nil {
							result = "Dismissed: " + msg.original.ID
						}
					}
				}
			default:
				result = "Saved (no status change)"
			}
			if apiErr != nil {
				m.history = append(m.history, chatEntry{kindError, "Apply error: " + apiErr.Error()})
			} else if result != "" {
				m.history = append(m.history, chatEntry{kindResult, result})
			}
		}
		m.screen = screenChat
		m.vp.SetContent(m.renderHistory())
		m.vp.GotoBottom()

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
		// Any printable char: switch to chat/insert and start typing.
		if len(msg.Runes) == 1 {
			m.screen = screenChat
			m.input.enterInsert()
			m.input.insertChar(msg.Runes[0])
			m.tabIndex = -1
			if m.ready {
				m.vp.Height = m.viewportHeight()
				m.vp.SetContent(m.renderHistory())
				m.vp.GotoBottom()
			}
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

	case "ctrl+c", "ctrl+d":
		return m, tea.Quit

	case "enter":
		line := strings.TrimSpace(m.input.value())
		if line == "" {
			return m, nil
		}
		// Handle edit <n> specially: needs access to today items.
		if cmd, handled := m.handleEditCommand(line); handled {
			m.history = append(m.history, chatEntry{kindEcho, line})
			m.input.clear()
			m.tabIndex = -1
			m.vp.SetContent(m.renderHistory())
			m.vp.GotoBottom()
			return m, cmd
		}
		m.history = append(m.history, chatEntry{kindEcho, line})
		m.input.clear()
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
		base := m.inputCompletionBase() // capture before input is modified
		if len(candidates) == 1 {
			m.setInputTo(base + candidates[0] + " ")
			m.tabIndex = -1
		} else {
			m.tabIndex = (m.tabIndex + 1) % len(candidates)
			m.setInputTo(base + candidates[m.tabIndex])
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

func (m *Model) setInputTo(s string) {
	m.input.clear()
	for _, r := range s {
		m.input.insertChar(r)
	}
}

// handleEditCommand detects "/edit <n>" and returns the editor tea.Cmd.
// Returns (nil, false) if the line is not an edit command.
func (m Model) handleEditCommand(line string) (tea.Cmd, bool) {
	bare := strings.TrimPrefix(line, "/") // tolerate optional leading slash
	name, args := parseLine(bare)
	if !strings.HasPrefix("edit", name) || name == "" {
		return nil, false
	}
	if len(args) == 0 {
		return func() tea.Msg {
			return inputResultMsg{err: fmt.Errorf("usage: /edit <number>")}
		}, true
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		return func() tea.Msg {
			return inputResultMsg{err: fmt.Errorf("invalid item number: %s", args[0])}
		}, true
	}
	items := m.flatTodayItems()
	if n > len(items) {
		return func() tea.Msg {
			return inputResultMsg{err: fmt.Errorf("item %d not found", n)}
		}, true
	}
	item := items[n-1]
	if item.proposal == nil {
		return func() tea.Msg {
			return inputResultMsg{err: fmt.Errorf("item %d is not editable", n)}
		}, true
	}
	return editItemCmd(item.proposal), true
}

// parseEditedStatus extracts the status value from YAML+md content bytes.
func parseEditedStatus(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "status:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "status:"))
		}
	}
	return ""
}

// runApproveByID calls the approve endpoint for a numeric-string ID.
func runApproveByID(numID string, api *APIClient) (string, error) {
	id, err := strconv.Atoi(numID)
	if err != nil {
		return "", fmt.Errorf("invalid id: %s", numID)
	}
	return runApprove(context.Background(), []string{strconv.Itoa(id)}, api)
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
		if i == m.tabIndex {
			parts[i] = tabSelStyle.Render(name)
		} else {
			parts[i] = tabDimStyle.Render(name)
		}
	}
	return "  " + strings.Join(parts, "  ")
}

func welcomeText() string {
	return echoStyle.Render("Type a command or 'help'. Press esc for normal mode.")
}

func (m Model) View() string {
	if !m.ready {
		return ""
	}

	sep := separatorStyle.Render(strings.Repeat("─", m.width))

	if m.screen == screenToday {
		hint := hintStyle.Render("  1-9 select  ·  esc chat")
		return strings.Join([]string{m.vp.View(), sep, hint}, "\n")
	}

	// Chat mode: prompt arrow points right (insert) or left (normal)
	promptChar := "❯"
	if m.input.mode == modeNormal {
		promptChar = "❮"
	}
	inputLine := fmt.Sprintf("%s %s",
		promptStyle.Render(promptChar),
		m.input.view(),
	)
	parts := []string{m.vp.View(), sep, inputLine, m.statusView()}
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

// execEditor opens a file path in $EDITOR via tea.ExecProcess.
func execEditor(path string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return inputResultMsg{err: fmt.Errorf("editor: %w", err)}
		}
		return inputResultMsg{output: "Saved: " + path}
	})
}

// execFzfNotes opens fzf with a file list; on select, sends noteSelectedMsg to
// open the file in $EDITOR via a proper tea.ExecProcess chain.
func execFzfNotes(files []string) tea.Cmd {
	tmpIn, err := os.CreateTemp("", "clara-notes-in-*")
	if err != nil {
		e := err
		return func() tea.Msg { return inputResultMsg{err: e} }
	}
	fmt.Fprint(tmpIn, strings.Join(files, "\n"))
	tmpIn.Close()

	tmpOut, err := os.CreateTemp("", "clara-notes-out-*")
	if err != nil {
		os.Remove(tmpIn.Name())
		e := err
		return func() tea.Msg { return inputResultMsg{err: e} }
	}
	tmpOut.Close()

	// fzf with bat/cat preview, preview window on right.
	shellCmd := fmt.Sprintf(
		"fzf --preview %s --preview-window=right:60%% < %s > %s",
		shellEscape("cat {}"),
		shellEscape(tmpIn.Name()),
		shellEscape(tmpOut.Name()),
	)
	cmd := exec.Command("sh", "-c", shellCmd)

	return tea.ExecProcess(cmd, func(_ error) tea.Msg {
		defer os.Remove(tmpIn.Name())
		defer os.Remove(tmpOut.Name())

		selBytes, readErr := os.ReadFile(tmpOut.Name())
		path := strings.TrimSpace(string(selBytes))
		if readErr != nil || path == "" {
			return inputResultMsg{output: "Notes: cancelled."}
		}
		// Return a message; model.Update will chain to execEditor.
		return noteSelectedMsg{path: path}
	})
}

// execFzfReminders opens fzf with formatted reminder lines.
// In normal mode, selection returns reminderSelectedMsg for viewport display.
// In editMode, selection returns reminderEditSelectedMsg to open in $EDITOR.
func execFzfReminders(items []reminderItem, editMode bool) tea.Cmd {
	// Build display lines: "<index>  [list] title"
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = fmt.Sprintf("%d\t[%s] %s", i, it.List, it.Title)
	}

	tmpIn, err := os.CreateTemp("", "clara-rem-in-*")
	if err != nil {
		e := err
		return func() tea.Msg { return inputResultMsg{err: e} }
	}
	fmt.Fprint(tmpIn, strings.Join(lines, "\n"))
	tmpIn.Close()

	tmpOut, err := os.CreateTemp("", "clara-rem-out-*")
	if err != nil {
		os.Remove(tmpIn.Name())
		e := err
		return func() tea.Msg { return inputResultMsg{err: e} }
	}
	tmpOut.Close()

	shellCmd := fmt.Sprintf(
		"fzf --with-nth=2.. --delimiter='\t' < %s > %s",
		shellEscape(tmpIn.Name()),
		shellEscape(tmpOut.Name()),
	)
	cmd := exec.Command("sh", "-c", shellCmd)

	return tea.ExecProcess(cmd, func(_ error) tea.Msg {
		defer os.Remove(tmpIn.Name())
		defer os.Remove(tmpOut.Name())

		selBytes, readErr := os.ReadFile(tmpOut.Name())
		line := strings.TrimSpace(string(selBytes))
		if readErr != nil || line == "" {
			return inputResultMsg{output: "Reminders: cancelled."}
		}
		// Parse the index from the first tab-delimited field.
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 0 {
			return inputResultMsg{output: "Reminders: cancelled."}
		}
		idx, parseErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		if parseErr != nil || idx < 0 || idx >= len(items) {
			return inputResultMsg{err: fmt.Errorf("reminders: invalid selection")}
		}
		if editMode {
			return reminderEditSelectedMsg{item: items[idx]}
		}
		return reminderSelectedMsg{item: items[idx]}
	})
}
