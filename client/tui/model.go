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

"github.com/charmbracelet/bubbles/viewport"
tea "github.com/charmbracelet/bubbletea"
"github.com/charmbracelet/lipgloss"
)

// ---- Styles -----------------------------------------------------------------

var (
tabActiveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
tabInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
tabSepStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

itemCursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
itemSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
itemNormalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))

loadingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
divStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
searchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
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
preview string // YAML/markdown content for the right pane
ref     any    // underlying typed value (for editing)
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

// ---- Layout helpers ---------------------------------------------------------

const (
headerLines = 1
footerLines = 1
divWidth    = 1
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
}

// ---- Data loading -----------------------------------------------------------

func (m Model) loadView(v viewMode) tea.Cmd {
switch v {
case viewSuggestions:
return m.loadSuggestions()
case viewNotes:
return loadNotes(m.cfg)
case viewTasks:
return loadTasks()
case viewStatus:
return m.loadStatus()
}
return nil
}

func (m Model) loadSuggestions() tea.Cmd {
return func() tea.Msg {
proposals, err := m.api.GetProposals(context.Background())
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

func (m Model) loadStatus() tea.Cmd {
return func() tea.Msg {
s, err := m.api.GetStatus(context.Background())
if err != nil {
return itemsLoadedMsg{view: viewStatus, err: err}
}
preview := fmt.Sprintf(
"---\nstatus: %s\nuptime: %s\n---\n\nDocuments:   %d\n\nSuggestions:\n  pending:  %d\n  approved: %d\n  rejected: %d\n",
s.Status, s.Uptime, s.Documents,
s.Suggestions.Pending, s.Suggestions.Approved, s.Suggestions.Rejected,
)
items := []listItem{{title: "Server", preview: preview}}
return itemsLoadedMsg{view: viewStatus, items: items}
}
}

func loadNotes(cfg localConfig) tea.Cmd {
return func() tea.Msg {
findArgs := []string{
"-L", cfg.NotesDir,
"(", "-name", "*.md", "-o", "-name", "*.markdown", ")",
"-not", "-path", "*/.git/*",
}
out, err := exec.Command("find", findArgs...).Output()
if err != nil {
return itemsLoadedMsg{view: viewNotes, err: fmt.Errorf("find notes: %w", err)}
}
files := strings.Split(strings.TrimSpace(string(out)), "\n")
if len(files) == 0 || (len(files) == 1 && files[0] == "") {
return itemsLoadedMsg{view: viewNotes}
}
sort.Strings(files)
items := make([]listItem, 0, len(files))
for _, f := range files {
name := filepath.Base(f)
var preview string
if content, readErr := os.ReadFile(f); readErr != nil {
preview = fmt.Sprintf("(error reading file: %v)", readErr)
} else {
preview = string(content)
}
items = append(items, listItem{title: name, preview: preview, ref: f})
}
return itemsLoadedMsg{view: viewNotes, items: items}
}
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
return itemsLoadedMsg{view: viewTasks, items: items}
}
}

// ---- Formatting helpers -----------------------------------------------------

func proposalTitle(p *ClaraItemJSON) string {
src := filepath.Base(p.Source)
src = strings.TrimSuffix(src, filepath.Ext(src))
if p.SourceRef != "" {
return fmt.Sprintf("%s → [[%s]]", src, p.SourceRef)
}
// Fall back to first non-empty line of body.
if p.Body != "" {
for _, line := range strings.Split(p.Body, "\n") {
if t := strings.TrimSpace(line); t != "" {
return t
}
}
}
return p.ID
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

// proposalExpandText renders a proposal for editing in $EDITOR (alias for proposalPreview).
func proposalExpandText(p *ClaraItemJSON) string {
return proposalPreview(p)
}

// extractNumericID returns the numeric part of "suggestion-42" → "42".
func extractNumericID(id string) string {
parts := strings.SplitN(id, "-", 2)
if len(parts) == 2 {
return parts[1]
}
return id
}

// parseEditedStatus extracts the status field value from YAML frontmatter.
func parseEditedStatus(content []byte) string {
for _, line := range strings.Split(string(content), "\n") {
line = strings.TrimSpace(line)
if strings.HasPrefix(line, "status:") {
return strings.TrimSpace(strings.TrimPrefix(line, "status:"))
}
}
return ""
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
view := m.view
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
// Reload suggestions after the action.
proposals, err := api.GetProposals(context.Background())
if err != nil {
return itemsLoadedMsg{view: view, err: err}
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
return itemsLoadedMsg{view: view, items: items}
}
}
// For note/task edits or errors, simply reload.
m.loading = true
return m, m.loadView(m.view)

case tea.KeyMsg:
if m.searching {
return m.handleSearchKey(msg)
}
return m.handleNormalKey(msg)
}

// Pass other messages (mouse, etc.) to the preview viewport.
var cmd tea.Cmd
m.preview, cmd = m.preview.Update(msg)
return m, cmd
}

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
m.preview.SetContent(m.filtered[m.selected].preview)
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
m.updatePreview()
}

case "k", "up":
if m.selected > 0 {
m.selected--
m.updatePreview()
}

case "ctrl+d":
if len(m.filtered) > 0 {
m.selected = min(m.selected+m.bodyHeight()/2, len(m.filtered)-1)
m.updatePreview()
}

case "ctrl+u":
m.selected = max(m.selected-m.bodyHeight()/2, 0)
m.updatePreview()

case "g":
m.selected = 0
m.updatePreview()

case "G":
if len(m.filtered) > 0 {
m.selected = len(m.filtered) - 1
m.updatePreview()
}

case "[":
m.view = viewMode((int(m.view) - 1 + len(viewNames)) % len(viewNames))
m.loading = true
m.items = nil
m.filtered = nil
m.selected = 0
m.searchQuery = ""
return m, m.loadView(m.view)

case "]":
m.view = viewMode((int(m.view) + 1) % len(viewNames))
m.loading = true
m.items = nil
m.filtered = nil
m.selected = 0
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
m.updatePreview()
}

case "enter":
return m.editSelected()

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
m.updatePreview()

case "enter":
m.searching = false
// Keep current filter active.

case "backspace":
if len(m.searchQuery) > 0 {
runes := []rune(m.searchQuery)
m.searchQuery = string(runes[:len(runes)-1])
m.applyFilter()
m.updatePreview()
}

default:
if len(msg.Runes) == 1 {
m.searchQuery += string(msg.Runes)
m.applyFilter()
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
leftLines := m.buildLeftLines()
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
// Pad left line to exact width (lipgloss handles ANSI-aware padding).
sb.WriteString(lipgloss.NewStyle().Width(lw).MaxWidth(lw).Render(ll))
sb.WriteString(div)
if i < len(rightLines) {
sb.WriteString(rightLines[i])
}
}
return sb.String()
}

func (m Model) buildLeftLines() []string {
if m.loading {
return []string{loadingStyle.Render("  Loading…")}
}
if m.loadErr != "" {
return []string{errStyle.Render("  Error: " + m.loadErr)}
}
if len(m.filtered) == 0 {
if m.searchQuery != "" {
return []string{loadingStyle.Render("  (no matches)")}
}
return []string{loadingStyle.Render("  (no items)")}
}
lw := m.leftWidth()
lines := make([]string, 0, len(m.filtered))
for i, item := range m.filtered {
title := truncate(item.title, lw-4)
if i == m.selected {
lines = append(lines, itemCursorStyle.Render("▶")+" "+itemSelectedStyle.Render(title))
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
return lipgloss.NewStyle().Width(m.width).Render(prompt + m.searchQuery + cursor)
}
hints := "  j/k navigate  ·  enter edit  ·  [ ] cycle views  ·  / search  ·  r reload  ·  q quit"
return statusStyle.Render(lipgloss.NewStyle().Width(m.width).Render(hints))
}
