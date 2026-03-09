package panes

import (
"fmt"
"os"
"strings"
"time"

tea "github.com/charmbracelet/bubbletea"
"github.com/charmbracelet/lipgloss"

agentv1 "github.com/brightpuddle/clara/gen/agent/v1"
artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
"github.com/brightpuddle/clara/internal/artifact"
configpkg "github.com/brightpuddle/clara/internal/config"
"github.com/brightpuddle/clara/tui/styles"
)

// DetailPane renders the preview and metadata for the selected artifact,
// or a settings view when a settings category is active.
type DetailPane struct {
artifact         *artifactv1.Artifact
focused          bool
width            int
height           int
scrollY          int
settingsCategory string
statusData       *agentv1.GetStatusResponse
statusFetchedAt  time.Time // when statusData was last fetched (for local uptime counting)
uptimeTick       int64     // increments each second via TickUptime
config           *configpkg.Config
// vim nav when focused
searching      bool
searchBuf      string
searchMatches  []int
searchIdx      int
lastKey        string
}

// NewDetailPane creates an empty DetailPane.
func NewDetailPane() DetailPane {
return DetailPane{}
}

// SetArtifact sets the artifact to display and clears any settings view.
func (p *DetailPane) SetArtifact(a *artifactv1.Artifact) {
p.artifact = a
p.scrollY = 0
p.settingsCategory = ""
}

// GetArtifact returns the currently displayed artifact, or nil if none.
func (p *DetailPane) GetArtifact() *artifactv1.Artifact {
return p.artifact
}

// SetSettingsView switches the pane to show the named settings category.
func (p *DetailPane) SetSettingsView(category string, statusData *agentv1.GetStatusResponse, cfg *configpkg.Config) {
p.settingsCategory = category
p.statusData = statusData
p.config = cfg
p.scrollY = 0
if statusData != nil {
p.statusFetchedAt = time.Now()
p.uptimeTick = 0
}
}

// TickUptime increments the local uptime counter by one second.
// Call this every second while the status view is visible to avoid polling the server.
func (p *DetailPane) TickUptime() {
if p.statusData != nil {
p.uptimeTick++
}
}

// SetSize sets the pane dimensions.
func (p *DetailPane) SetSize(w, h int) {
p.width = w
p.height = h
}

// SetFocused sets whether this pane has focus.
func (p *DetailPane) SetFocused(f bool) {
p.focused = f
}

// ScrollDown scrolls the detail view down.
func (p *DetailPane) ScrollDown() {
p.scrollY++
}

// ScrollUp scrolls the detail view up.
func (p *DetailPane) ScrollUp() {
if p.scrollY > 0 {
p.scrollY--
}
}

// View renders the detail pane.
func (p *DetailPane) View() string {
if p.width <= 0 || p.height <= 0 {
return ""
}

if p.settingsCategory != "" {
return p.renderSettings()
}

return p.renderArtifact()
}

func (p *DetailPane) renderSettings() string {
switch p.settingsCategory {
case "status":
return p.renderStatusView()
case "config":
return p.renderConfigView()
default:
return p.renderEmptySettings(p.settingsCategory)
}
}

func (p *DetailPane) border() lipgloss.Style {
if p.focused {
return styles.FocusedBorder
}
return styles.UnfocusedBorder
}

func (p *DetailPane) renderStatusView() string {
innerW := p.width - 4
if innerW < 1 {
innerW = 1
}

var lines []string
lines = append(lines, styles.Bold.Render("Components"))
lines = append(lines, "")

if p.statusData == nil {
lines = append(lines, styles.Muted.Render("  loading…"))
} else {
renderComp := func(name string, c *agentv1.ComponentStatus) {
if c == nil {
return
}
connStr := "✗ disconnected"
connColor := styles.Muted
if c.Connected {
connStr = "✓ " + c.State
connColor = styles.ItemNormal
}
lines = append(lines, styles.ItemNormal.Render(fmt.Sprintf("  %-10s %s", name+":", connColor.Render(connStr))))
uptimeSecs := c.UptimeSeconds
if c.Connected && name == "agent" {
uptimeSecs += p.uptimeTick
}
if c.Connected && uptimeSecs > 0 {
lines = append(lines, styles.Muted.Render(fmt.Sprintf("  %-10s %s", "", formatUptime(uptimeSecs))))
}
if c.Fault != "" {
lines = append(lines, styles.Muted.Render(fmt.Sprintf("  %-10s %s", "", c.Fault)))
}
}
renderComp("agent", p.statusData.Agent)
renderComp("native", p.statusData.Native)

if len(p.statusData.ArtifactCounts) > 0 {
lines = append(lines, "")
lines = append(lines, styles.Bold.Render("Artifact Counts"))
lines = append(lines, "")
for kind, count := range p.statusData.ArtifactCounts {
lines = append(lines, styles.ItemNormal.Render(fmt.Sprintf("  %-14s %d", kind+":", count)))
}
}
}

return p.wrapInBorder("Status", lines, innerW)
}


func (p *DetailPane) renderConfigView() string {
innerW := p.width - 4
if innerW < 1 {
innerW = 1
}

configPath := configpkg.ConfigPath()
var lines []string

lines = append(lines, styles.Bold.Render("Config file: "+truncateStr(configPath, innerW-14)))
lines = append(lines, styles.Muted.Render("  Enter: edit in $EDITOR   o: open in app"))
lines = append(lines, "")

content, err := os.ReadFile(configPath)
if err != nil {
lines = append(lines, styles.Muted.Render("  (file not found — defaults are active)"))
} else {
for _, line := range strings.Split(string(content), "\n") {
if strings.HasPrefix(line, "#") {
lines = append(lines, styles.Muted.Render(line))
} else if strings.Contains(line, ":") {
colonIdx := strings.Index(line, ":")
key := line[:colonIdx]
val := line[colonIdx:]
lines = append(lines, styles.ItemNormal.Render(styles.Bold.Render(key)+val))
} else {
lines = append(lines, styles.ItemNormal.Render(line))
}
}
}

return p.wrapInBorder("Config", lines, innerW)
}

func (p *DetailPane) renderEmptySettings(category string) string {
innerW := p.width - 4
lines := []string{styles.Muted.Render("  unknown category: " + category)}
return p.wrapInBorder("Settings", lines, innerW)
}

func (p *DetailPane) wrapInBorder(title string, lines []string, innerW int) string {
innerH := p.height - 3 - 1
if innerH < 1 {
innerH = 1
}

start := p.scrollY
if start >= len(lines) && len(lines) > 0 {
start = len(lines) - 1
}
end := start + innerH
if end > len(lines) {
end = len(lines)
}

var visLines []string
for _, line := range lines[start:end] {
visLines = append(visLines, styles.ItemNormal.Width(innerW).Render(truncateStr(line, innerW)))
}

body := strings.Join(visLines, "\n")
rendered := p.border().Width(p.width - 2).Height(p.height - 2).Render(body)

displayTitle := title
if p.searching {
displayTitle = "/ " + p.searchBuf + "█"
}
return styles.InjectBorderTitle(rendered, "0", displayTitle, p.width, p.focused)
}

func (p *DetailPane) renderArtifact() string {
borderStyle := styles.UnfocusedBorder
if p.focused {
borderStyle = styles.FocusedBorder
}

if p.artifact == nil {
empty := styles.Muted.Render("  Select an artifact to preview")
rendered := borderStyle.Width(p.width - 2).Height(p.height - 2).Render(empty)
return styles.InjectBorderTitle(rendered, "0", "Detail", p.width, p.focused)
}

a := p.artifact
icon := artifact.KindIcon(a.Kind)
color := kindColor(a.Kind)

titleLine := lipgloss.NewStyle().Foreground(color).Bold(true).
Render(fmt.Sprintf("%s  %s", icon, a.Title))

var meta []string
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("kind:   %s", kindName(a.Kind))))
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("heat:   %s %.2f", styles.HeatBar(a.HeatScore), a.HeatScore)))
if a.SourcePath != "" {
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("source: %s", truncateStr(a.SourcePath, p.width-12))))
}
if a.SourceApp != "" {
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("app:    %s", a.SourceApp)))
}
if a.DueAt != nil {
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("due:    %s", a.DueAt.AsTime().Format("2006-01-02 15:04"))))
}
if len(a.Tags) > 0 {
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("tags:   %s", strings.Join(a.Tags, ", "))))
}
if a.Kind == artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER {
if list, ok := a.Metadata["list"]; ok && list != "" {
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("list:   %s", list)))
}
if pri, ok := a.Metadata["priority"]; ok {
priNames := map[string]string{"0": "none", "1": "high", "5": "medium", "9": "low"}
if name, ok := priNames[pri]; ok {
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("priority: %s", name)))
} else {
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("priority: %s", pri)))
}
}
}
if a.Kind == artifactv1.ArtifactKind_ARTIFACT_KIND_TASK {
if proj, ok := a.Metadata["project"]; ok && proj != "" {
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("project: %s", proj)))
}
if pri, ok := a.Metadata["priority"]; ok && pri != "" {
meta = append(meta, styles.ItemNormal.Render(fmt.Sprintf("priority: %s", pri)))
}
}

separator := strings.Repeat("─", p.width-4)

contentLines := strings.Split(a.Content, "\n")
innerH := p.height - 3 - len(meta) - 3
if innerH < 1 {
innerH = 1
}

start := p.scrollY
if start >= len(contentLines) {
start = maxInt(0, len(contentLines)-1)
}
end := start + innerH
if end > len(contentLines) {
end = len(contentLines)
}

var contentRows []string
for _, line := range contentLines[start:end] {
contentRows = append(contentRows, styles.ItemNormal.Width(p.width-4).Render(truncateStr(line, p.width-4)))
}

parts := []string{titleLine, ""}
parts = append(parts, meta...)
parts = append(parts, styles.Muted.Render(separator))
parts = append(parts, contentRows...)

inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
rendered := borderStyle.Width(p.width - 2).Height(p.height - 2).Render(inner)

artTitle := truncateStr(a.Title, 40)
displayTitle := artTitle
if p.searching {
displayTitle = "/ " + p.searchBuf + "█"
}
return styles.InjectBorderTitle(rendered, "0", displayTitle, p.width, p.focused)
}

func kindName(kind artifactv1.ArtifactKind) string {
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
case artifactv1.ArtifactKind_ARTIFACT_KIND_SUGGESTION:
return "suggestion"
case artifactv1.ArtifactKind_ARTIFACT_KIND_TASK:
return "task"
default:
return "unknown"
}
}

func maxInt(a, b int) int {
if a > b {
return a
}
return b
}

func formatUptime(seconds int64) string {
if seconds < 60 {
return fmt.Sprintf("%ds", seconds)
}
if seconds < 3600 {
return fmt.Sprintf("%dm%ds", seconds/60, seconds%60)
}
return fmt.Sprintf("%dh%dm", seconds/3600, (seconds%3600)/60)
}

// IsSearching returns true if the detail pane is in search mode.
func (p *DetailPane) IsSearching() bool { return p.searching }

// HandleKey processes vim navigation keys when the detail pane is focused.
// Returns true if the key was consumed.
func (p *DetailPane) HandleKey(msg tea.KeyMsg) bool {
if p.searching {
return p.handleSearchKey(msg)
}

totalLines := p.totalContentLines()
visibleLines := p.height - 3

switch msg.String() {
case "j", "down":
p.scrollY++
return true
case "k", "up":
if p.scrollY > 0 {
p.scrollY--
}
return true
case "g":
if p.lastKey == "g" {
p.scrollY = 0
p.lastKey = ""
} else {
p.lastKey = "g"
}
return true
case "G":
p.lastKey = ""
if totalLines > visibleLines {
p.scrollY = totalLines - visibleLines
}
return true
case "/":
p.lastKey = ""
p.searching = true
p.searchBuf = ""
p.searchMatches = nil
p.searchIdx = 0
return true
case "n":
p.lastKey = ""
if len(p.searchMatches) > 0 {
p.searchIdx = (p.searchIdx + 1) % len(p.searchMatches)
p.scrollY = p.searchMatches[p.searchIdx]
}
return true
case "p":
p.lastKey = ""
if len(p.searchMatches) > 0 {
p.searchIdx = (p.searchIdx - 1 + len(p.searchMatches)) % len(p.searchMatches)
p.scrollY = p.searchMatches[p.searchIdx]
}
return true
case "esc":
p.lastKey = ""
p.searching = false
p.searchBuf = ""
p.searchMatches = nil
return false // signal model to unfocus detail pane
}
p.lastKey = ""
return false
}

func (p *DetailPane) handleSearchKey(msg tea.KeyMsg) bool {
switch msg.String() {
case "esc", "ctrl+c":
p.searching = false
p.searchBuf = ""
p.searchMatches = nil
case "enter":
p.searching = false
case "backspace":
if len(p.searchBuf) > 0 {
p.searchBuf = p.searchBuf[:len(p.searchBuf)-1]
}
p.computeSearchMatches()
default:
if len(msg.Runes) > 0 {
p.searchBuf += string(msg.Runes)
p.computeSearchMatches()
}
}
return true
}

func (p *DetailPane) computeSearchMatches() {
if p.searchBuf == "" {
p.searchMatches = nil
p.searchIdx = 0
return
}
q := strings.ToLower(p.searchBuf)
lines := p.contentLines()
p.searchMatches = nil
for i, line := range lines {
if strings.Contains(strings.ToLower(line), q) {
p.searchMatches = append(p.searchMatches, i)
}
}
p.searchIdx = 0
if len(p.searchMatches) > 0 {
p.scrollY = p.searchMatches[0]
}
}

// contentLines returns all lines of current content for search purposes.
func (p *DetailPane) contentLines() []string {
if p.settingsCategory == "config" {
content, err := os.ReadFile(configpkg.ConfigPath())
if err != nil {
return nil
}
return strings.Split(string(content), "\n")
}
if p.artifact != nil {
return strings.Split(p.artifact.Content, "\n")
}
return nil
}

func (p *DetailPane) totalContentLines() int {
return len(p.contentLines())
}
