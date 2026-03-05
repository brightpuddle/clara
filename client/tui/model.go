package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- Styles -----------------------------------------------------------------

const (
	selBgColor  = lipgloss.Color("4")  // selection pill background
	selFgColor  = lipgloss.Color("15") // selection pill foreground
	dimColor    = lipgloss.Color("8")
	textColor   = lipgloss.Color("7")
	activeColor = lipgloss.Color("12")
	errorColor  = lipgloss.Color("1")

	// Nerd Font Powerline rounded caps (requires Nerd Font terminal font).
	// U+E0B6 = left-facing rounded cap (right end of pill)
	// U+E0B4 = right-facing rounded cap (left end of pill)
	pillLeft  = "\ue0b6"
	pillRight = "\ue0b4"
)

var (
	tabActiveStyle   = lipgloss.NewStyle().Foreground(activeColor).Bold(true)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(dimColor)
	tabSepStyle      = lipgloss.NewStyle().Foreground(dimColor)

	itemNormalStyle = lipgloss.NewStyle().Foreground(textColor)
	// Pill cap: same foreground as selBgColor so caps blend with the content block.
	pillCapStyle  = lipgloss.NewStyle().Foreground(selBgColor)
	pillBodyStyle = lipgloss.NewStyle().Background(selBgColor).Foreground(selFgColor).Bold(true)

	loadingStyle = lipgloss.NewStyle().Foreground(dimColor).Italic(true)
	errStyle     = lipgloss.NewStyle().Foreground(errorColor).Bold(true)
	divStyle     = lipgloss.NewStyle().Foreground(dimColor)
	statusStyle  = lipgloss.NewStyle().Foreground(dimColor)
	searchStyle  = lipgloss.NewStyle().Foreground(activeColor).Bold(true)
	keyStyle     = lipgloss.NewStyle().Foreground(activeColor)
	hintStyle    = lipgloss.NewStyle().Foreground(dimColor)
)

// ---- Views ------------------------------------------------------------------

type viewMode int

const (
	viewSuggestions viewMode = iota
	viewNotes
	viewTasks
	viewStatus
)

var viewNames = []string{"Suggestions", "Notes", "Tasks", "Status"}

// ---- Data -------------------------------------------------------------------

// listItem is an entry in the left pane.
type listItem struct {
	title   string // short display label
	preview string // YAML/markdown content for the right pane (may be loaded lazily)
	ref     any    // underlying typed value (file path, taskItem, *ClaraItemJSON, etc.)
}

// itemsLoadedMsg is delivered when a view's data finishes loading.
type itemsLoadedMsg struct {
	view  viewMode
	items []listItem
	err   error
}

// ---- Model ------------------------------------------------------------------

// Model is the root Bubbletea model for the 2-pane TUI.
type Model struct {
	api           *APIClient
	cfg           localConfig
	width, height int
	ready         bool

	view     viewMode
	items    []listItem // all loaded items for current view
	filtered []listItem // subset after search filter
	selected int
	offset   int // first visible index in filtered list (for scrolling)

	loading bool
	loadErr string

	searching   bool
	searchQuery string

	preview viewport.Model
}

func New(api *APIClient) Model {
	return Model{
		api:     api,
		cfg:     readLocalConfig(),
		view:    viewSuggestions,
		loading: true,
	}
}

func (m Model) Init() tea.Cmd {
	return m.loadView(m.view)
}

// ---- Layout -----------------------------------------------------------------

const (
	headerLines = 1
	footerLines = 1
	divWidth    = 1
	scrollPad   = 5 // yazi-style: keep this many items visible above/below cursor
)

func (m Model) bodyHeight() int {
	h := m.height - headerLines - footerLines
	if h < 1 {
		h = 1
	}
	return h
}

func (m Model) leftWidth() int {
	w := m.width * 30 / 100
	if w < 22 {
		w = 22
	}
	if w > 48 {
		w = 48
	}
	return w
}

func (m Model) rightWidth() int {
	rw := m.width - m.leftWidth() - divWidth
	if rw < 1 {
		rw = 1
	}
	return rw
}

// adjustScroll updates m.offset so the selected row stays within scrollPad
// of each edge (yazi-style context padding). Compresses near the list ends.
func (m *Model) adjustScroll() {
	bh := m.bodyHeight()
	n := len(m.filtered)
	if n <= bh {
		m.offset = 0
		return
	}
	// Too close to top edge → scroll up
	if m.selected-m.offset < scrollPad {
		m.offset = max(m.selected-scrollPad, 0)
	}
	// Too close to bottom edge → scroll down
	if m.offset+bh-m.selected <= scrollPad {
		m.offset = min(m.selected-bh+scrollPad+1, n-bh)
	}
	// Clamp
	m.offset = max(m.offset, 0)
	m.offset = min(m.offset, n-bh)
}

// ---- Fuzzy filter -----------------------------------------------------------

// fuzzyMatch reports whether all runes in query appear in s in order (case-insensitive).
func fuzzyMatch(s, query string) bool {
	if query == "" {
		return true
	}
	s = strings.ToLower(s)
	q := strings.ToLower(query)
	si := 0
	for _, qr := range q {
		found := false
		for si < len(s) {
			if rune(s[si]) == qr {
				si++
				found = true
				break
			}
			si++
		}
		if !found {
			return false
		}
	}
	return true
}

func (m *Model) applyFilter() {
	if m.searchQuery == "" {
		m.filtered = m.items
		return
	}
	result := make([]listItem, 0, len(m.items))
	for _, item := range m.items {
		if fuzzyMatch(item.title, m.searchQuery) {
			result = append(result, item)
		}
	}
	m.filtered = result
	if m.selected >= len(m.filtered) {
		m.selected = 0
	}
	m.offset = 0
}

// ---- Data loading -----------------------------------------------------------

func (m Model) loadView(v viewMode) tea.Cmd {
	switch v {
	case viewSuggestions:
		return loadSuggestions(m.api)
	case viewNotes:
		return loadNotes(m.cfg)
	case viewTasks:
		return loadTasks()
	case viewStatus:
		return loadStatus(m.api)
	}
	return nil
}

func loadSuggestions(api *APIClient) tea.Cmd {
	return func() tea.Msg {
		proposals, err := api.GetProposals(context.Background())
		if err != nil {
			return itemsLoadedMsg{view: viewSuggestions, err: err}
		}
		items := make([]listItem, 0, len(proposals))
		for i := range proposals {
			p := &proposals[i]
			items = append(items, listItem{
				title:   proposalTitle(p),
				preview: proposalPreview(p),
				ref:     p,
			})
		}
		return itemsLoadedMsg{view: viewSuggestions, items: items}
	}
}

func loadStatus(api *APIClient) tea.Cmd {
	return func() tea.Msg {
		type serverResult struct {
			status ServerStatus
			err    error
		}
		type agentResult struct {
			text string
			err  error
		}

		sCh := make(chan serverResult, 1)
		aCh := make(chan agentResult, 1)

		go func() {
			s, err := api.GetStatus(context.Background())
			sCh <- serverResult{s, err}
		}()
		go func() {
			text, err := agentSocketStatus(context.Background())
			aCh <- agentResult{text, err}
		}()

		sr := <-sCh
		ar := <-aCh

		// Server item
		var serverTitle, serverPreview string
		if sr.err != nil {
			serverTitle = "Server  (offline)"
			serverPreview = "---\nstatus: offline\n---\n\n" + sr.err.Error() + "\n"
		} else {
			s := sr.status
			serverTitle = fmt.Sprintf("Server  (%s)", s.Status)
			serverPreview = fmt.Sprintf(
				"---\nstatus: %s\nuptime: %s\n---\n\nDocuments: %d\n\nSuggestions:\n  pending:  %d\n  approved: %d\n  rejected: %d\n",
				s.Status, s.Uptime, s.Documents,
				s.Suggestions.Pending, s.Suggestions.Approved, s.Suggestions.Rejected,
			)
		}

		// Agent item — agentSocketStatus never returns an error; offline is in the text.
		agentTitle := "Agent"
		agentPreview := ar.text
		if ar.err != nil {
			agentPreview = "---\nstatus: offline\n---\n\n" + ar.err.Error() + "\n"
		}
		if strings.Contains(agentPreview, "running") {
			agentTitle = "Agent  (running)"
		} else {
			agentTitle = "Agent  (offline)"
		}

		items := []listItem{
			{title: serverTitle, preview: serverPreview},
			{title: agentTitle, preview: agentPreview},
		}
		return itemsLoadedMsg{view: viewStatus, items: items}
	}
}

// loadNotes lists markdown files using walkMarkdownFiles (follows symlinks, detects cycles).
// File content is loaded lazily in updatePreview to keep the initial load fast.
func loadNotes(cfg localConfig) tea.Cmd {
	return func() tea.Msg {
		files, err := walkMarkdownFiles(cfg.NotesDir)
		if err != nil {
			return itemsLoadedMsg{view: viewNotes, err: fmt.Errorf("walk notes: %w", err)}
		}
		sort.Strings(files)
		items := make([]listItem, 0, len(files))
		for _, f := range files {
			name := filepath.Base(f)
			items = append(items, listItem{title: name, ref: f}) // preview loaded lazily
		}
		return itemsLoadedMsg{view: viewNotes, items: items}
	}
}

// dueDateFormats are tried in order when parsing reminders due dates.
var dueDateFormats = []string{
	time.RFC3339,
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02",
	"Jan 2, 2006 at 3:04 PM",
	"Jan 2, 2006",
}

func parseDueDate(s string) (time.Time, bool) {
	for _, f := range dueDateFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func loadTasks() tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("reminders", "show-all", "--format", "json").Output()
		if err != nil {
			return itemsLoadedMsg{view: viewTasks, err: fmt.Errorf("reminders: %w", err)}
		}
		var all []taskItem
		if err := json.Unmarshal(out, &all); err != nil {
			return itemsLoadedMsg{view: viewTasks, err: fmt.Errorf("parse tasks: %w", err)}
		}
		items := make([]listItem, 0, len(all))
		for _, t := range all {
			if !t.IsCompleted {
				ti := t
				items = append(items, listItem{
					title:   fmt.Sprintf("[%s] %s", t.List, t.Title),
					preview: taskToClaraItem(ti),
					ref:     ti,
				})
			}
		}
		// Sort by due date ascending (overdue + today first); no-date items last.
		sort.SliceStable(items, func(i, j int) bool {
			di := items[i].ref.(taskItem).DueDate
			dj := items[j].ref.(taskItem).DueDate
			ti, iOk := parseDueDate(di)
			tj, jOk := parseDueDate(dj)
			if !iOk && !jOk {
				return false
			}
			if !iOk {
				return false // i has no date → sort last
			}
			if !jOk {
				return true // j has no date → sort last
			}
			return ti.Before(tj)
		})
		return itemsLoadedMsg{view: viewTasks, items: items}
	}
}

// ---- Formatting helpers -----------------------------------------------------

// suggestionIcon returns a Nerd Font icon for the given suggestion type.
func suggestionIcon(suggType string) string {
	switch suggType {
	case "add_backlink":
		return "\uf0c1" // nf-fa-link
	default:
		return "\uf111" // nf-fa-circle (generic)
	}
}

func proposalTitle(p *ClaraItemJSON) string {
	icon := suggestionIcon(p.Type)
	linkName := p.SourceRef
	if linkName == "" {
		// Fall back to first non-empty line of body.
		if p.Body != "" {
			for _, line := range strings.Split(p.Body, "\n") {
				if t := strings.TrimSpace(line); t != "" {
					linkName = t
					break
				}
			}
		}
		if linkName == "" {
			return icon + " " + p.ID
		}
	}
	// If SourceRef looks like a path, use only the base name without extension.
	if strings.Contains(linkName, "/") || strings.Contains(linkName, "\\") {
		linkName = filepath.Base(linkName)
		linkName = strings.TrimSuffix(linkName, filepath.Ext(linkName))
	}
	return fmt.Sprintf("%s  %s", icon, linkName)
}

func proposalPreview(p *ClaraItemJSON) string {
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
		sb.WriteByte('\n')
		sb.WriteString(p.Body)
		if !strings.HasSuffix(p.Body, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// proposalExpandText is used by editItemCmd in commands.go.
func proposalExpandText(p *ClaraItemJSON) string { return proposalPreview(p) }

// extractNumericID returns the numeric part of "suggestion-42" → "42".
func extractNumericID(id string) string {
	parts := strings.SplitN(id, "-", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return id
}

// parseEditedStatus extracts the status field value from YAML frontmatter bytes.
func parseEditedStatus(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "status:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "status:"))
		}
	}
	return ""
}

// truncateRunes truncates s to at most n runes, appending "…" if truncated.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	return string(runes[:n-1]) + "…"
}

// ---- Update -----------------------------------------------------------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.ready {
			m.preview = viewport.New(m.rightWidth(), m.bodyHeight())
			m.ready = true
		} else {
			m.preview.Width = m.rightWidth()
			m.preview.Height = m.bodyHeight()
		}
		m.updatePreview()
		return m, nil

	case itemsLoadedMsg:
		if msg.view != m.view {
			return m, nil // stale result for a view we've left
		}
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err.Error()
			m.items = nil
			m.filtered = nil
		} else {
			m.loadErr = ""
			m.items = msg.items
			m.filtered = msg.items
		}
		m.selected = 0
		m.offset = 0
		m.searchQuery = ""
		m.searching = false
		if m.ready {
			m.preview.Width = m.rightWidth()
			m.preview.Height = m.bodyHeight()
		}
		m.updatePreview()
		return m, nil

	case editDoneMsg:
		// After editing a suggestion, optionally apply an approve/dismiss action.
		if msg.err == nil && msg.original != nil {
			api := m.api
			numID := extractNumericID(msg.original.ID)
			action := parseEditedStatus(msg.content)
			return m, func() tea.Msg {
				switch action {
				case "approved":
					if id, err := strconv.ParseInt(numID, 10, 64); err == nil {
						api.Approve(context.Background(), id) //nolint:errcheck
					}
				case "dismissed":
					if id, err := strconv.ParseInt(numID, 10, 64); err == nil {
						api.Dismiss(context.Background(), id) //nolint:errcheck
					}
				}
				return loadSuggestions(api)()
			}
		}
		// For note/task edits (or errors), reload.
		m.loading = true
		return m, m.loadView(m.view)

	case tea.KeyMsg:
		if m.searching {
			return m.handleSearchKey(msg)
		}
		return m.handleNormalKey(msg)
	}

	var cmd tea.Cmd
	m.preview, cmd = m.preview.Update(msg)
	return m, cmd
}

// updatePreview refreshes the right pane for the currently selected item.
// For notes, file content is read lazily on first selection.
func (m *Model) updatePreview() {
	if len(m.filtered) == 0 {
		m.preview.SetContent("")
		return
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
	}

	content := m.filtered[m.selected].preview

	// Lazy-load note content on first selection.
	if content == "" && m.view == viewNotes {
		if path, ok := m.filtered[m.selected].ref.(string); ok {
			if data, err := os.ReadFile(path); err == nil {
				content = string(data)
				m.filtered[m.selected].preview = content // cache
				// Also update in m.items so the cache survives filter resets.
				title := m.filtered[m.selected].title
				for i := range m.items {
					if m.items[i].title == title {
						m.items[i].preview = content
						break
					}
				}
			} else {
				content = fmt.Sprintf("(error reading file: %v)", err)
			}
		}
	}

	m.preview.SetContent(content)
	m.preview.GotoTop()
}

// ---- Key handlers -----------------------------------------------------------

func (m Model) handleNormalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "j", "down":
		if m.selected < len(m.filtered)-1 {
			m.selected++
			m.adjustScroll()
			m.updatePreview()
		}

	case "k", "up":
		if m.selected > 0 {
			m.selected--
			m.adjustScroll()
			m.updatePreview()
		}

	case "ctrl+d":
		if len(m.filtered) > 0 {
			m.selected = min(m.selected+m.bodyHeight()/2, len(m.filtered)-1)
			m.adjustScroll()
			m.updatePreview()
		}

	case "ctrl+u":
		m.selected = max(m.selected-m.bodyHeight()/2, 0)
		m.adjustScroll()
		m.updatePreview()

	case "g":
		m.selected = 0
		m.offset = 0
		m.updatePreview()

	case "G":
		if len(m.filtered) > 0 {
			m.selected = len(m.filtered) - 1
			m.adjustScroll()
			m.updatePreview()
		}

	case "h":
		m.view = viewMode((int(m.view) - 1 + len(viewNames)) % len(viewNames))
		m.loading = true
		m.items = nil
		m.filtered = nil
		m.selected = 0
		m.offset = 0
		m.searchQuery = ""
		return m, m.loadView(m.view)

	case "l":
		m.view = viewMode((int(m.view) + 1) % len(viewNames))
		m.loading = true
		m.items = nil
		m.filtered = nil
		m.selected = 0
		m.offset = 0
		m.searchQuery = ""
		return m, m.loadView(m.view)

	case "/":
		m.searching = true
		m.searchQuery = ""

	case "esc":
		if m.searchQuery != "" || m.searching {
			m.searching = false
			m.searchQuery = ""
			m.filtered = m.items
			m.selected = 0
			m.offset = 0
			m.updatePreview()
		}

	case "enter":
		return m.editSelected()

	case "a":
		if m.view == viewSuggestions && len(m.filtered) > 0 {
			return m.suggestionAction(true)
		}

	case "d":
		if m.view == viewSuggestions && len(m.filtered) > 0 {
			return m.suggestionAction(false)
		}

	case "r":
		m.loading = true
		m.loadErr = ""
		return m, m.loadView(m.view)

	// Preview pane scrolling
	case "ctrl+f":
		m.preview.HalfViewDown()
	case "ctrl+b":
		m.preview.HalfViewUp()
	}
	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.searchQuery = ""
		m.filtered = m.items
		m.selected = 0
		m.offset = 0
		m.updatePreview()

	case "enter":
		m.searching = false // keep filter active

	case "backspace":
		if len(m.searchQuery) > 0 {
			runes := []rune(m.searchQuery)
			m.searchQuery = string(runes[:len(runes)-1])
			m.applyFilter()
			m.adjustScroll()
			m.updatePreview()
		}

	default:
		if len(msg.Runes) == 1 {
			m.searchQuery += string(msg.Runes)
			m.applyFilter()
			m.adjustScroll()
			m.updatePreview()
		}
	}
	return m, nil
}

func (m Model) editSelected() (Model, tea.Cmd) {
	if len(m.filtered) == 0 || m.selected >= len(m.filtered) {
		return m, nil
	}
	item := m.filtered[m.selected]
	switch m.view {
	case viewNotes:
		if path, ok := item.ref.(string); ok {
			return m, execEditor(path)
		}
	case viewTasks:
		if task, ok := item.ref.(taskItem); ok {
			return m, execTaskEditor(task)
		}
	case viewSuggestions:
		if p, ok := item.ref.(*ClaraItemJSON); ok {
			return m, editItemCmd(p)
		}
	}
	return m, nil
}

// suggestionAction accepts (approve=true) or dismisses (approve=false) the
// currently selected suggestion, then reloads the suggestions list.
func (m Model) suggestionAction(approve bool) (Model, tea.Cmd) {
	if len(m.filtered) == 0 || m.selected >= len(m.filtered) {
		return m, nil
	}
	p, ok := m.filtered[m.selected].ref.(*ClaraItemJSON)
	if !ok {
		return m, nil
	}
	numID := extractNumericID(p.ID)
	api := m.api
	m.loading = true
	return m, func() tea.Msg {
		if id, err := strconv.ParseInt(numID, 10, 64); err == nil {
			if approve {
				api.Approve(context.Background(), id) //nolint:errcheck
			} else {
				api.Dismiss(context.Background(), id) //nolint:errcheck
			}
		}
		return loadSuggestions(api)()
	}
}

// execEditor opens path in $EDITOR via tea.ExecProcess.
func execEditor(path string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editDoneMsg{err: err}
	})
}

// ---- Rendering --------------------------------------------------------------

func (m Model) View() string {
	if !m.ready {
		return ""
	}
	return m.renderHeader() + "\n" + m.renderBody() + "\n" + m.renderFooter()
}

func (m Model) renderHeader() string {
	sep := tabSepStyle.Render("  ")
	var parts []string
	for i, name := range viewNames {
		if viewMode(i) == m.view {
			parts = append(parts, tabActiveStyle.Render(name))
		} else {
			parts = append(parts, tabInactiveStyle.Render(name))
		}
	}
	line := "  " + strings.Join(parts, sep)
	return lipgloss.NewStyle().Width(m.width).Render(line)
}

func (m Model) renderBody() string {
	lw := m.leftWidth()
	bh := m.bodyHeight()
	leftLines := m.buildLeftLines(bh)
	rightContent := m.preview.View()
	rightLines := strings.Split(rightContent, "\n")

	div := divStyle.Render("│")
	var sb strings.Builder
	for i := 0; i < bh; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		var ll string
		if i < len(leftLines) {
			ll = leftLines[i]
		}
		sb.WriteString(lipgloss.NewStyle().Width(lw).MaxWidth(lw).Render(ll))
		sb.WriteString(div)
		if i < len(rightLines) {
			sb.WriteString(rightLines[i])
		}
	}
	return sb.String()
}

// renderSelectedLine renders the selected list item with an inverted rounded pill.
// Visual: [selBg-colored left cap][inverted content][selBg-colored right cap]
func renderSelectedLine(title string) string {
	return pillCapStyle.Render(pillLeft) +
		pillBodyStyle.Render(" "+title+" ") +
		pillCapStyle.Render(pillRight)
}

func (m Model) buildLeftLines(height int) []string {
	if m.loading {
		return []string{loadingStyle.Render("  Loading…")}
	}
	if m.loadErr != "" {
		return []string{errStyle.Render("  " + m.loadErr)}
	}
	if len(m.filtered) == 0 {
		if m.searchQuery != "" {
			return []string{loadingStyle.Render("  (no matches)")}
		}
		return []string{loadingStyle.Render("  (no items)")}
	}

	// Each pill takes: 1 cap + 1 space + title + 1 space + 1 cap = title + 4
	// Normal items: 2 spaces + title
	lw := m.leftWidth()
	titleWidth := lw - 4 // for selected pill caps + padding

	lines := make([]string, 0, height)
	end := min(m.offset+height, len(m.filtered))
	for i := m.offset; i < end; i++ {
		title := truncateRunes(m.filtered[i].title, titleWidth)
		if i == m.selected {
			lines = append(lines, renderSelectedLine(title))
		} else {
			lines = append(lines, "  "+itemNormalStyle.Render(title))
		}
	}
	return lines
}

func (m Model) renderFooter() string {
	if m.searching {
		cursor := searchStyle.Render("█")
		prompt := searchStyle.Render("/ ")
		line := prompt + m.searchQuery + cursor
		return lipgloss.NewStyle().Width(m.width).Render(line)
	}

	hint := func(key, desc string) string {
		return keyStyle.Render(key) + hintStyle.Render(" "+desc)
	}

	parts := []string{
		hint("j/k", "navigate"),
		hint("enter", "edit"),
		hint("h/l", "views"),
		hint("/", "search"),
		hint("r", "reload"),
	}
	if m.view == viewSuggestions {
		parts = append(parts, hint("a", "accept"), hint("d", "dismiss"))
	}
	parts = append(parts, hint("q", "quit"))

	sep := hintStyle.Render("  ·  ")
	line := "  " + strings.Join(parts, sep)
	return lipgloss.NewStyle().Width(m.width).Render(line)
}
