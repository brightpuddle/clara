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
"google.golang.org/protobuf/types/known/timestamppb"

agentv1 "github.com/brightpuddle/clara/gen/agent/v1"
artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
configpkg "github.com/brightpuddle/clara/internal/config"
"github.com/brightpuddle/clara/internal/theme"
tuigrpc "github.com/brightpuddle/clara/tui/grpc"
"github.com/brightpuddle/clara/tui/panes"
"github.com/brightpuddle/clara/tui/styles"
)

type focusedPane int

const (
paneArtifacts focusedPane = iota
paneRelated
paneSettings
paneDetail
)

const PaneCount = 3

type paneRect struct{ x, y, w, h int }

// helpItems are the status bar entries in priority order (most important first).
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
type settingsViewMsg struct {
category   string
statusData *agentv1.GetStatusResponse
}
type configReloadedMsg struct{ cfg *configpkg.Config }
type uptimeTickMsg struct{}

type reminderUpdateMsg struct {
id      string
title   string
notes   string
dueDate *timestamppb.Timestamp
}

// Model is the root Bubbletea model for the Clara TUI.
type Model struct {
client   *tuigrpc.Client
ctx      context.Context
cancel   context.CancelFunc
logger   zerolog.Logger
themeMgr *theme.Manager
cfg      *configpkg.Config

artifacts panes.ArtifactsPane
related   panes.RelatedPane
settings  panes.SettingsPane
detail    panes.DetailPane

focus  focusedPane
width  int
height int
status string
err    string

artifactsRect paneRect
relatedRect   paneRect
settingsRect  paneRect
detailRect    paneRect
}

// New creates a new TUI Model.
func New(client *tuigrpc.Client, logger zerolog.Logger, mgr *theme.Manager, cfg *configpkg.Config) Model {
ctx, cancel := context.WithCancel(context.Background())
m := Model{
client:    client,
ctx:       ctx,
cancel:    cancel,
logger:    logger,
themeMgr:  mgr,
cfg:       cfg,
artifacts: panes.NewArtifactsPane(),
related:   panes.NewRelatedPane(),
settings:  panes.NewSettingsPane(),
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

case tea.MouseMsg:
return m.handleMouse(msg)

case tea.KeyMsg:
return m.handleKey(msg)

case artifactsLoadedMsg:
m.artifacts.SetArtifacts(msg.artifacts)
m.status = fmt.Sprintf("loaded %d artifacts", len(msg.artifacts))
// Don't overwrite settings detail view when syncing.
if m.focus != paneSettings {
if sel := m.artifacts.Selected(); sel != nil {
return m, m.loadDetail(sel.Id)
}
}

case artifactDetailMsg:
// Don't overwrite settings detail when syncing in background.
if m.focus != paneSettings {
m.detail.SetArtifact(msg.artifact)
m.related.SetRelated(msg.related)
}

case agentDisconnectedMsg:
m.status = "⚠ disconnected from agent — retrying…"
return m, m.retrySubscribeCmd()

case artifactEventMsg:
if msg.event == nil {
m.status = "reconnected"
}
return m, tea.Batch(m.loadArtifacts(), waitForEventCmd(msg.ch))

case themeChangedMsg:
m.themeMgr.SetDark(msg.isDark)
styles.SetTheme(m.themeMgr.Current())
return m, m.pollTheme()

case statusMsg:
m.status = msg.text

case errorMsg:
m.err = msg.err.Error()

case settingsViewMsg:
m.detail.SetSettingsView(msg.category, msg.statusData, m.cfg)
if msg.category == "status" && msg.statusData != nil {
return m, uptimeTickCmd()
}

case uptimeTickMsg:
m.detail.TickUptime()
if m.focus == paneSettings {
return m, uptimeTickCmd()
}

case configReloadedMsg:
m.cfg = msg.cfg
m.detail.SetSettingsView("config", nil, m.cfg)
m.status = "config reloaded"

case reminderUpdateMsg:
return m, m.callUpdateReminder(msg)
}

return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
switch msg.String() {
case "ctrl+c", "ctrl+d", "q":
if !m.isSearching() {
m.cancel()
return m, tea.Quit
}
case "tab":
return m, m.cycleFocus(1)
case "shift+tab":
return m, m.cycleFocus(-1)
case "1":
if !m.isSearching() {
return m, m.setFocus(paneArtifacts)
}
case "2":
if !m.isSearching() {
return m, m.setFocus(paneRelated)
}
case "3":
if !m.isSearching() {
return m, m.setFocus(paneSettings)
}
case "0":
if !m.isSearching() {
return m, m.setFocus(paneDetail)
}
case "l":
// l moves to the next pane (down/right)
if m.focus == paneArtifacts {
return m, m.setFocus(paneRelated)
} else if m.focus == paneRelated {
return m, m.setFocus(paneSettings)
} else if m.focus == paneSettings {
return m, m.setFocus(paneArtifacts)
}
case "h":
// h moves to the previous pane (up/left)
if m.focus == paneArtifacts {
return m, m.setFocus(paneSettings)
} else if m.focus == paneRelated {
return m, m.setFocus(paneArtifacts)
} else if m.focus == paneSettings {
return m, m.setFocus(paneRelated)
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

case paneSettings:
// Handle 'o' to open config in native app.
if msg.String() == "o" {
if sel := m.settings.Selected(); sel != nil && sel.ID == "config" {
return m, m.openConfigNative()
}
}
action := m.settings.Update(msg)
if action != "" {
return m, m.handleAction(action)
}

case paneDetail:
handled := m.detail.HandleKey(msg)
if !handled && msg.String() == "esc" {
return m, m.setFocus(paneArtifacts)
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
case "settings":
// id is "nav:CATEGORY" or "edit:CATEGORY"
subIdx := strings.Index(id, ":")
if subIdx < 0 {
return m.showSettings(id)
}
subVerb := id[:subIdx]
category := id[subIdx+1:]
if subVerb == "edit" {
return m.editConfig(category)
}
return m.showSettings(category)
}
return nil
}

// autoShowSettings immediately sets the settings view for the given category
// without going through the tea.Cmd round-trip (used on focus entry).
func (m *Model) autoShowSettings(category string) {
if category == "config" {
m.detail.SetSettingsView("config", nil, m.cfg)
return
}
// For status, we need async fetch — just clear to trigger a loading state.
m.detail.SetSettingsView(category, nil, m.cfg)
}

func (m *Model) showSettings(category string) tea.Cmd {
return func() tea.Msg {
if category == "status" {
resp, err := m.client.GetStatus(m.ctx)
if err != nil {
return settingsViewMsg{category: category}
}
return settingsViewMsg{category: category, statusData: resp}
}
return settingsViewMsg{category: category}
}
}

func (m *Model) editConfig(category string) tea.Cmd {
if category != "config" {
// Status: just refresh.
return m.showSettings(category)
}
cfgPath := configpkg.ConfigPath()
// Ensure the yaml-language-server modeline is present.
ensureConfigModeline(cfgPath, configpkg.SchemaPath())

editor := os.Getenv("EDITOR")
if editor == "" {
editor = "vim"
}
cmd := exec.Command(editor, cfgPath) //nolint:gosec
cmd.Stdin = os.Stdin
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
return tea.ExecProcess(cmd, func(err error) tea.Msg {
if err != nil {
return errorMsg{err}
}
// Reload config from disk.
newCfg, loadErr := configpkg.Load()
if loadErr != nil {
return errorMsg{loadErr}
}
return configReloadedMsg{cfg: newCfg}
})
}

func (m *Model) openConfigNative() tea.Cmd {
cmd := exec.Command("open", configpkg.ConfigPath()) //nolint:gosec
return func() tea.Msg {
if err := cmd.Start(); err != nil {
return errorMsg{err}
}
return statusMsg{"opened config in native app"}
}
}

// ensureConfigModeline adds the yaml-language-server modeline to the top
// of the config file if it is not already present.
func ensureConfigModeline(cfgPath, schemaPath string) {
data, err := os.ReadFile(cfgPath)
if err != nil {
return
}
modeline := "# yaml-language-server: $schema=file://" + schemaPath
if strings.Contains(string(data), "yaml-language-server") {
return
}
newContent := modeline + "\n" + string(data)
	if err := os.WriteFile(cfgPath, []byte(newContent), 0o644); err != nil {
		// Log but don't fail - schema is optional
	}
}

func (m *Model) openInEditor(id string) tea.Cmd {
sel := m.findArtifact(id)
if sel == nil {
return nil
}

path := sel.SourcePath
cleanup := false
isReminder := sel.SourceApp == "reminders"

if isReminder || path == "" || !fileExists(path) {
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
artifactSourcePath := sel.SourcePath
cmd := exec.Command(editor, path) //nolint:gosec
cmd.Stdin = os.Stdin
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
return tea.ExecProcess(cmd, func(err error) tea.Msg {
if err != nil {
if cleanup {
os.Remove(path) //nolint:errcheck
}
return errorMsg{err}
}
if cleanup && isReminder {
content, readErr := os.ReadFile(path)
os.Remove(path) //nolint:errcheck
if readErr == nil {
fm := parseFrontmatter(string(content))
body := extractBody(string(content))
msg := reminderUpdateMsg{
id:    artifactSourcePath,
title: fm["title"],
notes: body,
}
if dueStr := fm["due"]; dueStr != "" {
if t, err := time.Parse("2006-01-02 15:04", dueStr); err == nil {
msg.dueDate = timestamppb.New(t)
}
}
return msg
}
} else if cleanup {
os.Remove(path) //nolint:errcheck
}
return statusMsg{"editor closed"}
})
}

func parseFrontmatter(content string) map[string]string {
fm := make(map[string]string)
if !strings.HasPrefix(content, "---\n") {
return fm
}
rest := content[4:]
end := strings.Index(rest, "\n---\n")
if end < 0 {
return fm
}
for _, line := range strings.Split(rest[:end], "\n") {
colonIdx := strings.Index(line, ":")
if colonIdx < 0 {
continue
}
key := strings.TrimSpace(line[:colonIdx])
val := strings.TrimSpace(line[colonIdx+1:])
val = strings.Trim(val, `"`)
fm[key] = val
}
return fm
}

func extractBody(content string) string {
if !strings.HasPrefix(content, "---\n") {
return content
}
rest := content[4:]
end := strings.Index(rest, "\n---\n")
if end < 0 {
return content
}
body := rest[end+5:]
return strings.TrimPrefix(body, "\n")
}

func (m *Model) callUpdateReminder(msg reminderUpdateMsg) tea.Cmd {
return func() tea.Msg {
if err := m.client.UpdateReminder(m.ctx, msg.id, msg.title, msg.notes, msg.dueDate); err != nil {
return errorMsg{err}
}
return statusMsg{"reminder updated"}
}
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
case artifactv1.ArtifactKind_ARTIFACT_KIND_TASK:
return "task"
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
if sel.SourcePath != "" {
script := fmt.Sprintf(
`tell application "Reminders"
activate
repeat with theList in every list
    set matches to (reminders of theList whose id is "%s")
    if (count of matches) > 0 then
        show item 1 of matches
        exit repeat
    end if
end repeat
end tell`, sel.SourcePath)
return func() tea.Msg {
osacmd := exec.Command("osascript", "-e", script) //nolint:gosec
if err := osacmd.Start(); err != nil {
_ = exec.Command("open", "-a", "Reminders").Start() //nolint:errcheck
}
return statusMsg{"opened reminder"}
}
}
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

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
// Handle scroll wheel (wheel events come as press events with special buttons).
if msg.Button == tea.MouseButtonWheelDown {
return m.handleMouseScroll(msg.X, msg.Y, 1)
}
if msg.Button == tea.MouseButtonWheelUp {
return m.handleMouseScroll(msg.X, msg.Y, -1)
}
switch msg.Action {
case tea.MouseActionPress:
if msg.Button == tea.MouseButtonLeft {
return m.handleMouseClick(msg.X, msg.Y)
}
}
return m, nil
}

func (m Model) handleMouseClick(x, y int) (tea.Model, tea.Cmd) {
switch {
case x >= m.detailRect.x && x < m.detailRect.x+m.detailRect.w &&
y >= m.detailRect.y && y < m.detailRect.y+m.detailRect.h:
return m, m.setFocus(paneDetail)

case x >= m.artifactsRect.x && x < m.artifactsRect.x+m.artifactsRect.w &&
y >= m.artifactsRect.y && y < m.artifactsRect.y+m.artifactsRect.h:
cmd := m.setFocus(paneArtifacts)
row := y - m.artifactsRect.y - 1 // -1 for top border
if m.artifacts.SelectAtRow(row) {
if sel := m.artifacts.Selected(); sel != nil {
return m, tea.Batch(cmd, m.loadDetail(sel.Id))
}
}
return m, cmd

case x >= m.relatedRect.x && x < m.relatedRect.x+m.relatedRect.w &&
y >= m.relatedRect.y && y < m.relatedRect.y+m.relatedRect.h:
cmd := m.setFocus(paneRelated)
row := y - m.relatedRect.y - 1
m.related.SelectAtRow(row)
return m, cmd

case x >= m.settingsRect.x && x < m.settingsRect.x+m.settingsRect.w &&
y >= m.settingsRect.y && y < m.settingsRect.y+m.settingsRect.h:
cmd := m.setFocus(paneSettings)
row := y - m.settingsRect.y - 1
if m.settings.SelectAtRow(row) {
if sel := m.settings.Selected(); sel != nil {
return m, tea.Batch(cmd, m.showSettings(sel.ID))
}
}
return m, cmd
}
return m, nil
}

func (m Model) handleMouseScroll(x, y, delta int) (tea.Model, tea.Cmd) {
switch {
case x >= m.detailRect.x && y < m.detailRect.h:
if delta > 0 {
m.detail.ScrollDown()
} else {
m.detail.ScrollUp()
}
case y >= m.artifactsRect.y && y < m.artifactsRect.y+m.artifactsRect.h:
if delta > 0 {
m.artifacts.ScrollDown()
} else {
m.artifacts.ScrollUp()
}
case y >= m.relatedRect.y && y < m.relatedRect.y+m.relatedRect.h:
if delta > 0 {
m.related.ScrollDown()
} else {
m.related.ScrollUp()
}
case y >= m.settingsRect.y && y < m.settingsRect.y+m.settingsRect.h:
if delta > 0 {
m.settings.ScrollDown()
} else {
m.settings.ScrollUp()
}
}
return m, nil
}

func (m *Model) isSearching() bool {
	if m.focus == paneArtifacts {
		return m.artifacts.IsSearching()
	}
	if m.focus == paneRelated {
		return m.related.IsSearching()
	}
	if m.focus == paneDetail {
		return m.detail.IsSearching()
	}
	return false
}

func (m *Model) cycleFocus(delta int) tea.Cmd {
return m.setFocus(focusedPane((int(m.focus) + delta + PaneCount) % PaneCount))
}

func (m *Model) setFocus(f focusedPane) tea.Cmd {
entering := f == paneSettings && m.focus != paneSettings
m.focus = f
m.artifacts.SetFocused(f == paneArtifacts)
m.related.SetFocused(f == paneRelated)
m.settings.SetFocused(f == paneSettings)
m.detail.SetFocused(f == paneDetail)
m.updatePaneSizes()
if entering {
// Auto-show the currently selected settings category and fetch data.
if sel := m.settings.Selected(); sel != nil {
m.autoShowSettings(sel.ID)
return m.showSettings(sel.ID)
}
}
return nil
}

func (m *Model) updatePaneSizes() {
if m.width == 0 || m.height == 0 {
return
}
sidebarW := m.width * 35 / 100
detailW := m.width - sidebarW

const collapsedH = 3
var artifactsH, relatedH, settingsH int

switch m.focus {
case paneArtifacts:
relatedH = collapsedH
settingsH = collapsedH
artifactsH = m.height - 2*collapsedH - 1
if artifactsH < 5 {
artifactsH = 5
}
case paneRelated:
artifactsH = collapsedH
settingsH = collapsedH
relatedH = m.height - 2*collapsedH - 1
if relatedH < 5 {
relatedH = 5
}
case paneSettings:
artifactsH = collapsedH
relatedH = collapsedH
settingsH = m.height - 2*collapsedH - 1
if settingsH < 5 {
settingsH = 5
}
default:
h3 := (m.height - 1) / 3
artifactsH = h3
relatedH = h3
settingsH = m.height - 1 - 2*h3
if settingsH < 3 {
settingsH = 3
}
}

m.artifacts.SetSize(sidebarW, artifactsH)
m.related.SetSize(sidebarW, relatedH)
m.settings.SetSize(sidebarW, settingsH)
m.detail.SetSize(detailW, m.height-1)

// Track pane positions for mouse hit-testing.
m.artifactsRect = paneRect{x: 0, y: 0, w: sidebarW, h: artifactsH}
m.relatedRect = paneRect{x: 0, y: artifactsH, w: sidebarW, h: relatedH}
m.settingsRect = paneRect{x: 0, y: artifactsH + relatedH, w: sidebarW, h: settingsH}
m.detailRect = paneRect{x: sidebarW, y: 0, w: m.width - sidebarW, h: m.height - 1}
}

func (m Model) View() string {
if m.width == 0 {
return "Loading…"
}

sidebarW := m.width * 35 / 100

sidebar := lipgloss.JoinVertical(lipgloss.Left,
m.artifacts.View(),
m.related.View(),
m.settings.View(),
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

func (m Model) retrySubscribeCmd() tea.Cmd {
return tea.Tick(5*time.Second, func(_ time.Time) tea.Msg {
ch, err := m.client.Subscribe(m.ctx)
if err != nil {
return agentDisconnectedMsg{}
}
return artifactEventMsg{event: nil, ch: ch}
})
}

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

func uptimeTickCmd() tea.Cmd {
return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
return uptimeTickMsg{}
})
}
