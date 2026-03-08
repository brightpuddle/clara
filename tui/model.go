// Package tui implements the Clara terminal user interface using Bubbletea.
package tui

import (
"context"
"fmt"
"os"
"os/exec"
"strings"
"time"

tea "github.com/charmbracelet/bubbletea"
"github.com/charmbracelet/lipgloss"
"github.com/rs/zerolog"

agentv1 "github.com/brightpuddle/clara/gen/agent/v1"
artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
"github.com/brightpuddle/clara/internal/theme"
tuigrpc "github.com/brightpuddle/clara/tui/grpc"
"github.com/brightpuddle/clara/tui/panes"
"github.com/brightpuddle/clara/tui/styles"
)

type focusedPane int

const (
paneArtifacts focusedPane = iota
paneRelated
paneDetail
)

const PaneCount = 2

// helpItems are the status bar entries in priority order (most important first).
// Lower-priority items are truncated first when space is tight.
var helpItems = []string{
"j/k:nav",
"/:search",
"Space:done",
"Enter:edit",
"o:open",
"q:quit",
}

// Msg types.
type artifactsLoadedMsg struct{ artifacts []*artifactv1.Artifact }
type artifactDetailMsg struct {
artifact *artifactv1.Artifact
related  []*artifactv1.Artifact
}
type artifactEventMsg struct {
	event *agentv1.ArtifactEvent
	ch    <-chan *agentv1.ArtifactEvent
}
type themeChangedMsg struct{ isDark bool }
type errorMsg struct{ err error }
type statusMsg struct{ text string }
type agentDisconnectedMsg struct{}
type agentReconnectedMsg struct{}

// Model is the root Bubbletea model for the Clara TUI.
type Model struct {
client   *tuigrpc.Client
ctx      context.Context
cancel   context.CancelFunc
logger   zerolog.Logger
themeMgr *theme.Manager

artifacts panes.ArtifactsPane
related   panes.RelatedPane
detail    panes.DetailPane

focus  focusedPane
width  int
height int
status string
err    string
}

// New creates a new TUI Model.
func New(client *tuigrpc.Client, logger zerolog.Logger, mgr *theme.Manager) Model {
ctx, cancel := context.WithCancel(context.Background())
m := Model{
client:    client,
ctx:       ctx,
cancel:    cancel,
logger:    logger,
themeMgr:  mgr,
artifacts: panes.NewArtifactsPane(),
related:   panes.NewRelatedPane(),
detail:    panes.NewDetailPane(),
focus:     paneArtifacts,
}
m.artifacts.SetFocused(true)
styles.SetTheme(mgr.Current())
return m
}

func (m Model) Init() tea.Cmd {
cmds := []tea.Cmd{
m.loadArtifacts(),
m.subscribeToAgent(),
}
if m.themeMgr != nil {
cmds = append(cmds, m.pollTheme())
}
return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
switch msg := msg.(type) {
case tea.WindowSizeMsg:
m.width = msg.Width
m.height = msg.Height
m.updatePaneSizes()

case tea.KeyMsg:
return m.handleKey(msg)

case artifactsLoadedMsg:
m.artifacts.SetArtifacts(msg.artifacts)
m.status = fmt.Sprintf("loaded %d artifacts", len(msg.artifacts))
if sel := m.artifacts.Selected(); sel != nil {
return m, m.loadDetail(sel.Id)
}

case artifactDetailMsg:
m.detail.SetArtifact(msg.artifact)
m.related.SetRelated(msg.related)

case agentDisconnectedMsg:
m.status = "⚠ disconnected from agent — retrying…"
return m, m.retrySubscribeCmd()

case artifactEventMsg:
if msg.event == nil {
	m.status = "reconnected"
}
// Reload artifact list and continue listening for the next event.
return m, tea.Batch(m.loadArtifacts(), waitForEventCmd(msg.ch))
case themeChangedMsg:
m.themeMgr.SetDark(msg.isDark)
styles.SetTheme(m.themeMgr.Current())
return m, m.pollTheme()

case statusMsg:
m.status = msg.text

case errorMsg:
m.err = msg.err.Error()
}

return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
switch msg.String() {
case "ctrl+c", "q":
if m.focus == paneArtifacts && !m.isSearching() {
m.cancel()
return m, tea.Quit
}
case "tab":
m.cycleFocus(1)
return m, nil
case "shift+tab":
m.cycleFocus(-1)
return m, nil
case "l", "h":
// l and h toggle between the two left panes only, wrapping around.
// Neither key enters the detail pane.
if m.focus == paneArtifacts {
m.setFocus(paneRelated)
return m, nil
} else if m.focus == paneRelated {
m.setFocus(paneArtifacts)
return m, nil
}
}

switch m.focus {
case paneArtifacts:
action := m.artifacts.Update(msg)
if action != "" {
return m, m.handleAction(action)
}
if sel := m.artifacts.Selected(); sel != nil {
return m, m.loadDetail(sel.Id)
}

case paneRelated:
action := m.related.Update(msg)
if action != "" {
return m, m.handleAction(action)
}

case paneDetail:
switch msg.String() {
case "j", "down":
m.detail.ScrollDown()
case "k", "up":
m.detail.ScrollUp()
}
}

return m, nil
}

func (m *Model) handleAction(action string) tea.Cmd {
colonIdx := strings.Index(action, ":")
if colonIdx < 0 {
return nil
}
verb := action[:colonIdx]
id := action[colonIdx+1:]

switch verb {
case "done":
return func() tea.Msg {
if err := m.client.MarkDone(m.ctx, id); err != nil {
return errorMsg{err}
}
return statusMsg{"marked done"}
}
case "edit":
return m.openInEditor(id)
case "open":
return m.openNative(id)
}
return nil
}

func (m *Model) openInEditor(id string) tea.Cmd {
sel := m.findArtifact(id)
if sel == nil {
return nil
}

path := sel.SourcePath
cleanup := false

// For non-file artifacts, write content to a temp file.
if sel.SourceApp == "reminders" || path == "" || !fileExists(path) {
tmp, err := writeTempArtifact(sel)
if err != nil {
return func() tea.Msg { return errorMsg{err} }
}
path = tmp
cleanup = true
}

editor := os.Getenv("EDITOR")
if editor == "" {
editor = "vim"
}
cmd := exec.Command(editor, path) //nolint:gosec
cmd.Stdin = os.Stdin
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
return tea.ExecProcess(cmd, func(err error) tea.Msg {
if cleanup {
os.Remove(path) //nolint:errcheck
}
if err != nil {
return errorMsg{err}
}
return statusMsg{"editor closed"}
})
}

func fileExists(path string) bool {
_, err := os.Stat(path)
return err == nil
}

func kindNameStr(kind artifactv1.ArtifactKind) string {
switch kind {
case artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER:
return "reminder"
case artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE:
return "note"
case artifactv1.ArtifactKind_ARTIFACT_KIND_FILE:
return "file"
case artifactv1.ArtifactKind_ARTIFACT_KIND_EMAIL:
return "email"
case artifactv1.ArtifactKind_ARTIFACT_KIND_BOOKMARK:
return "bookmark"
case artifactv1.ArtifactKind_ARTIFACT_KIND_LOG:
return "log"
default:
return kind.String()
}
}

func writeTempArtifact(a *artifactv1.Artifact) (string, error) {
var sb strings.Builder
sb.WriteString("---\n")
sb.WriteString(fmt.Sprintf("title: %q\n", a.Title))
sb.WriteString(fmt.Sprintf("kind: %s\n", kindNameStr(a.Kind)))
if a.SourceApp != "" {
sb.WriteString(fmt.Sprintf("source_app: %s\n", a.SourceApp))
}
if meta := a.Metadata; meta != nil {
for k, v := range meta {
sb.WriteString(fmt.Sprintf("%s: %q\n", k, v))
}
}
if a.DueAt != nil {
sb.WriteString(fmt.Sprintf("due: %s\n", a.DueAt.AsTime().Format("2006-01-02 15:04")))
}
if len(a.Tags) > 0 {
sb.WriteString(fmt.Sprintf("tags: [%s]\n", strings.Join(a.Tags, ", ")))
}
sb.WriteString("---\n\n")
sb.WriteString(a.Content)

tmp, err := os.CreateTemp("", "clara-*.md")
if err != nil {
return "", err
}
defer tmp.Close()
_, err = tmp.WriteString(sb.String())
return tmp.Name(), err
}


func (m *Model) openNative(id string) tea.Cmd {
sel := m.findArtifact(id)
if sel == nil {
return nil
}
var cmd *exec.Cmd
switch sel.SourceApp {
case "reminders":
cmd = exec.Command("open", "-a", "Reminders")
case "mail":
cmd = exec.Command("open", "-a", "Mail")
default:
if sel.SourcePath != "" {
cmd = exec.Command("open", sel.SourcePath)
}
}
if cmd == nil {
return nil
}
return func() tea.Msg {
if err := cmd.Start(); err != nil {
return errorMsg{err}
}
return statusMsg{"opened in native app"}
}
}

func (m *Model) findArtifact(id string) *artifactv1.Artifact {
if sel := m.artifacts.Selected(); sel != nil && sel.Id == id {
return sel
}
if sel := m.related.Selected(); sel != nil && sel.Id == id {
return sel
}
return nil
}

func (m *Model) isSearching() bool {
return false
}

func (m *Model) cycleFocus(delta int) {
m.setFocus(focusedPane((int(m.focus) + delta + PaneCount) % PaneCount))
}

func (m *Model) setFocus(f focusedPane) {
m.focus = f
m.artifacts.SetFocused(f == paneArtifacts)
m.related.SetFocused(f == paneRelated)
m.updatePaneSizes()
}
func (m *Model) updatePaneSizes() {
if m.width == 0 || m.height == 0 {
return
}
sidebarW := m.width * 35 / 100
detailW := m.width - sidebarW

var artifactsH, relatedH int
switch m.focus {
case paneArtifacts:
relatedH = 3
artifactsH = m.height - 3 - 1
if artifactsH < 5 {
artifactsH = 5
}
case paneRelated:
artifactsH = 3
relatedH = m.height - 3 - 1
if relatedH < 5 {
relatedH = 5
}
default:
artifactsH = m.height / 2
relatedH = m.height - artifactsH - 1
if artifactsH < 5 {
artifactsH = 5
}
if relatedH < 5 {
relatedH = 5
}
}

m.artifacts.SetSize(sidebarW, artifactsH)
m.related.SetSize(sidebarW, relatedH)
m.detail.SetSize(detailW, m.height-1)
}

func (m Model) View() string {
if m.width == 0 {
return "Loading…"
}

sidebarW := m.width * 35 / 100

sidebar := lipgloss.JoinVertical(lipgloss.Left,
m.artifacts.View(),
m.related.View(),
)
sidebar = lipgloss.NewStyle().Width(sidebarW).Render(sidebar)
detail := m.detail.View()
main := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, detail)

return lipgloss.JoinVertical(lipgloss.Left, main, m.renderStatusBar())
}

func (m Model) renderStatusBar() string {
statusText := m.status
if m.err != "" {
statusText = "ERR: " + m.err
}
prefix := fmt.Sprintf(" clara  %s  ", statusText)
prefixW := lipgloss.Width(prefix)

helpStr := buildHelpText(helpItems, m.width-prefixW)

return lipgloss.NewStyle().
Width(m.width).
Background(styles.ColorHelpBg).
Foreground(styles.ColorHelpFg).
Render(prefix + helpStr)
}

// buildHelpText renders help items separated by "  ", dropping whole items
// from the right when they don't fit within maxWidth.
func buildHelpText(items []string, maxWidth int) string {
if maxWidth <= 0 {
return ""
}
const sep = "  "
const ellipsis = "…"

for drop := 0; drop <= len(items); drop++ {
visible := items[:len(items)-drop]
if len(visible) == 0 {
return ""
}
suffix := ""
if drop > 0 {
suffix = ellipsis
}
full := strings.Join(visible, sep) + suffix
if lipgloss.Width(full) <= maxWidth {
return full
}
}
return ""
}

// Commands.

func (m Model) loadArtifacts() tea.Cmd {
return func() tea.Msg {
arts, err := m.client.ListArtifacts(m.ctx)
if err != nil {
return errorMsg{err}
}
return artifactsLoadedMsg{artifacts: arts}
}
}

func (m Model) loadDetail(id string) tea.Cmd {
return func() tea.Msg {
a, related, err := m.client.GetArtifact(m.ctx, id)
if err != nil {
return errorMsg{err}
}
return artifactDetailMsg{artifact: a, related: related}
}
}

func (m Model) subscribeToAgent() tea.Cmd {
return func() tea.Msg {
ch, err := m.client.Subscribe(m.ctx)
if err != nil {
return agentDisconnectedMsg{}
}
return waitForEvent(ch)
}
}

// waitForEventCmd returns a tea.Cmd that blocks until the next event arrives on ch.
func waitForEventCmd(ch <-chan *agentv1.ArtifactEvent) tea.Cmd {
return func() tea.Msg {
return waitForEvent(ch)
}
}

func waitForEvent(ch <-chan *agentv1.ArtifactEvent) tea.Msg {
ev, ok := <-ch
if !ok {
return agentDisconnectedMsg{}
}
return artifactEventMsg{event: ev, ch: ch}
}

// retrySubscribeCmd waits 5 seconds then attempts to re-subscribe to the agent.
func (m Model) retrySubscribeCmd() tea.Cmd {
return tea.Tick(5*time.Second, func(_ time.Time) tea.Msg {
ch, err := m.client.Subscribe(m.ctx)
if err != nil {
return agentDisconnectedMsg{}
}
return artifactEventMsg{event: nil, ch: ch}
})
}

// pollTheme schedules a periodic check of the system theme via the agent.
// It polls every 5 seconds and triggers a themeChangedMsg if the value changes.
func (m Model) pollTheme() tea.Cmd {
return tea.Tick(5*time.Second, func(_ time.Time) tea.Msg {
ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
defer cancel()
isDark, err := m.client.GetSystemTheme(ctx)
if err != nil {
return nil
}
return themeChangedMsg{isDark: isDark}
})
}
