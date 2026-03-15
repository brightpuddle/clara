package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type viewMode int

const (
	modePrompt viewMode = iota
	modeNotifications
)

type Model struct {
	cfg     *config.Config
	client  *IPCClient
	theme   Theme
	input   textinput.Model
	service *NotificationService

	width  int
	height int
	mode   viewMode

	statusHint string
	startLines []string
	commands   []CommandSpec
	history    *commandHistory
	matches    []CompletionItem
	selected   int
	offset     int
	counts     StatusCounts
	startBlank bool

	notifications     []Notification
	notificationIndex int
}

func NewModel(
	cfg *config.Config,
	client *IPCClient,
	service *NotificationService,
	startLines []string,
) *Model {
	input := textinput.New()
	input.Focus()
	input.Prompt = ""
	input.CharLimit = 0
	input.Width = 80
	input.Placeholder = "Type /help for commands"
	_ = input.Cursor.SetMode(cursor.CursorStatic)

	m := &Model{
		cfg:        cfg,
		client:     client,
		theme:      DetectTheme(),
		input:      input,
		service:    service,
		statusHint: "Use /help for commands · Ctrl-X opens $EDITOR · Ctrl-L clears the screen",
		startLines: append([]string(nil), startLines...),
		commands:   commandSpecs(),
		startBlank: true,
	}
	historyPath := filepath.Join(cfg.DataDir, "tui-history.json")
	history, err := loadCommandHistory(historyPath, maxCommandHistory)
	if err == nil {
		m.history = history
	} else {
		m.history = &commandHistory{path: historyPath, limit: maxCommandHistory}
		m.history.resetNavigation()
	}
	m.refreshMatches()
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		waitForNotification(m.service.Events()),
		fetchStatusCmd(m.client),
		statusTickCmd(),
	}
	if len(m.startLines) > 0 {
		m.startBlank = false
		for _, line := range m.startLines {
			if line == "" {
				continue
			}
			cmds = append(cmds, tea.Println(line))
		}
	}
	return tea.Batch(append(cmds, tea.ClearScreen)...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(10, msg.Width-4)
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	case notificationMsg:
		m.notifications = append(m.notifications, msg.Notification)
		m.notificationIndex = len(m.notifications) - 1
		m.startBlank = false
		return m, tea.Batch(tea.Println(renderNotificationLine(m.theme, msg.Notification)), waitForNotification(m.service.Events()))
	case commandResultMsg:
		cmds := []tea.Cmd{}
		if msg.output != "" {
			m.startBlank = false
			cmds = append(cmds, tea.Println(msg.output))
		}
		if msg.openNotifications {
			m.mode = modeNotifications
		}
		if msg.quit {
			cmds = append(cmds, tea.Quit)
		}
		return m, tea.Batch(cmds...)
	case commandErrorMsg:
		m.startBlank = false
		return m, tea.Println(m.theme.Dimmed(time.Now().Format(time.Kitchen)) + " " + msg.output)
	case editorFinishedMsg:
		if msg.err != nil {
			return m, tea.Println(msg.err.Error())
		}
		m.input.SetValue(msg.text)
		m.history.resetNavigation()
		m.refreshMatches()
		return m, nil
	case statusMsg:
		m.counts = msg.Counts
		m.refreshMatches()
		return m, nil
	case statusTickMsg:
		return m, tea.Batch(fetchStatusCmd(m.client), statusTickCmd())
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refreshMatches()
	return m, cmd
}

func (m *Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeNotifications {
		return m.updateNotificationsKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+d":
		return m, tea.Quit
	case "ctrl+l":
		return m, tea.ClearScreen
	case "ctrl+x":
		return m, openEditorCmd(m.input.Value())
	case "enter":
		return m.submitInput()
	case "tab":
		if strings.HasPrefix(m.input.Value(), "/") && len(m.matches) > 0 {
			insert := m.matches[m.selected].Insert
			m.input.SetValue(insert)
			m.input.SetCursor(len(insert))
			m.refreshMatches()
		}
		return m, nil
	case "up":
		m.input.SetValue(m.history.Previous(m.input.Value()))
		m.input.SetCursor(len(m.input.Value()))
		m.refreshMatches()
		return m, nil
	case "down":
		m.input.SetValue(m.history.Next())
		m.input.SetCursor(len(m.input.Value()))
		m.refreshMatches()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.history.resetNavigation()
	m.refreshMatches()
	return m, cmd
}

func (m *Model) updateNotificationsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modePrompt
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+d":
		return m, tea.Quit
	case "j", "down":
		if m.notificationIndex < len(m.notifications)-1 {
			m.notificationIndex++
		}
		return m, nil
	case "k", "up":
		if m.notificationIndex > 0 {
			m.notificationIndex--
		}
		return m, nil
	default:
		if len(m.notifications) == 0 {
			return m, nil
		}
		n := m.notifications[m.notificationIndex]
		if len(n.Actions) == 0 {
			return m, nil
		}
		index := numberKeyIndex(msg.String())
		if index < 0 || index >= len(n.Actions) {
			return m, nil
		}
		action := n.Actions[index]
		_ = m.service.Respond(n.ID, action.ID)
		m.notifications[m.notificationIndex].Status = "responded"
		m.notifications[m.notificationIndex].ResponseAction = action.ID
		m.startBlank = false
		return m, tea.Println(
			renderNotificationLine(
				m.theme,
				m.notifications[m.notificationIndex],
			) + " " + m.theme.Dimmed(
				"→ "+action.Title,
			),
		)
	}
}

func (m *Model) submitInput() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		return m, nil
	}
	echo := fmt.Sprintf("%s %s", m.theme.Magenta("❯"), value)
	m.input.SetValue("")
	m.input.SetCursor(0)
	_ = m.history.Add(value)
	m.refreshMatches()

	if strings.HasPrefix(value, "/") {
		m.startBlank = false
		return m, tea.Batch(
			tea.Println(echo),
			executeSlashCommand(m.client, m.theme, value, m.commands),
			fetchStatusCmd(m.client),
		)
	}

	return m, tea.Batch(
		tea.Println(echo),
		tea.Println("Plain prompt input is not available yet. Use /help for slash commands."),
	)
}

func (m *Model) View() string {
	if m.mode == modeNotifications {
		return m.renderNotificationsView()
	}

	var b strings.Builder
	if m.startBlank && m.height > 0 {
		spacerLines := max(0, m.height-4)
		if spacerLines > 0 {
			b.WriteString(strings.Repeat("\n", spacerLines))
		}
	}
	hr := horizontalRule(m.width)
	b.WriteString(hr)
	b.WriteString("\n")
	b.WriteString(m.theme.Magenta("❯"))
	b.WriteString(" ")
	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString(hr)
	b.WriteString("\n")
	b.WriteString(m.renderStatusArea())
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) renderStatusArea() string {
	if !strings.HasPrefix(m.input.Value(), "/") {
		return m.theme.Dimmed(
			fmt.Sprintf(
				"servers: %d | tools: %d | intents: %d",
				m.counts.Servers,
				m.counts.Tools,
				m.counts.Intents,
			),
		)
	}
	if len(m.matches) == 0 {
		return m.theme.Dimmed(
			fmt.Sprintf(
				"servers: %d | tools: %d | intents: %d",
				m.counts.Servers,
				m.counts.Tools,
				m.counts.Intents,
			),
		)
	}

	visible := 8
	lines := make([]string, 0, 10)
	canScrollUp := m.offset > 0
	canScrollDown := m.offset+visible < len(m.matches)
	up := "▲"
	down := "▼"
	if canScrollUp {
		up = m.theme.Cyan(up)
	} else {
		up = m.theme.Dimmed(up)
	}
	if canScrollDown {
		down = m.theme.Cyan(down)
	} else {
		down = m.theme.Dimmed(down)
	}
	lines = append(lines, up)
	for i := 0; i < visible; i++ {
		idx := m.offset + i
		if idx >= len(m.matches) {
			lines = append(lines, "")
			continue
		}
		path := truncateRunes(m.matches[idx].Display, max(10, m.width-1))
		if idx == m.selected {
			lines = append(lines, m.theme.Cyan(path))
		} else {
			lines = append(lines, m.theme.Dimmed(path))
		}
	}
	lines = append(lines, down)
	return strings.Join(lines, "\n")
}

func (m *Model) renderNotificationsView() string {
	var b strings.Builder
	hr := horizontalRule(m.width)
	b.WriteString(hr)
	b.WriteString("\nNotifications\n")
	b.WriteString(hr)
	b.WriteString("\n")

	if len(m.notifications) == 0 {
		b.WriteString(m.theme.Dimmed("No notifications yet."))
		b.WriteString("\n")
		b.WriteString(hr)
		b.WriteString("\n")
		b.WriteString(m.theme.Dimmed("Press Esc to return"))
		return b.String()
	}

	listHeight := max(3, m.height-6)
	start := max(0, min(m.notificationIndex-listHeight/2, len(m.notifications)-listHeight))
	end := min(len(m.notifications), start+listHeight)
	for i := start; i < end; i++ {
		line := truncateRunes(
			renderNotificationLine(m.theme, m.notifications[i]),
			max(10, m.width-2),
		)
		if i == m.notificationIndex {
			b.WriteString(m.theme.Cyan(">"))
			b.WriteString(" ")
			b.WriteString(line)
		} else {
			b.WriteString("  ")
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	b.WriteString(hr)
	b.WriteString("\n")
	selected := m.notifications[m.notificationIndex]
	footer := "Esc closes · j/k or ↑/↓ navigate"
	if len(selected.Actions) > 0 && selected.Status != "responded" {
		actions := make([]string, 0, len(selected.Actions))
		for i, action := range selected.Actions {
			actions = append(actions, fmt.Sprintf("[%d] %s", i+1, action.Title))
		}
		footer += " · " + strings.Join(actions, "  ")
	}
	b.WriteString(m.theme.Dimmed(footer))
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) refreshMatches() {
	if !strings.HasPrefix(m.input.Value(), "/") {
		m.matches = nil
		m.selected = 0
		m.offset = 0
		return
	}
	prevPath := ""
	if len(m.matches) > 0 && m.selected < len(m.matches) {
		prevPath = m.matches[m.selected].Display
	}
	m.matches = completeInput(m.client, m.input.Value(), m.commands)
	m.selected = 0
	for i, match := range m.matches {
		if match.Display == prevPath {
			m.selected = i
			break
		}
	}
	m.offset = 0
	m.ensureSelectionVisible()
}

func (m *Model) moveSelection(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.matches) {
		m.selected = len(m.matches) - 1
	}
	m.ensureSelectionVisible()
}

func (m *Model) ensureSelectionVisible() {
	const visible = 8
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+visible {
		m.offset = m.selected - visible + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func waitForNotification(ch <-chan NotificationEvent) tea.Cmd {
	return func() tea.Msg {
		evt := <-ch
		return notificationMsg(evt)
	}
}

type notificationMsg struct {
	Notification Notification
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func numberKeyIndex(s string) int {
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return -1
	}
	return int(s[0] - '1')
}

type statusMsg struct {
	Counts StatusCounts
}

type statusTickMsg time.Time

func fetchStatusCmd(client *IPCClient) tea.Cmd {
	return func() tea.Msg {
		counts, err := client.StatusCounts()
		if err != nil {
			return statusMsg{}
		}
		return statusMsg{Counts: counts}
	}
}

func statusTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return statusTickMsg(t)
	})
}
