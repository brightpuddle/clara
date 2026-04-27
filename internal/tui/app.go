package tui

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/brightpuddle/clara/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modelSnapshot is a point-in-time copy of the content model state,
// safe to read from any goroutine (e.g. tests inspecting TUI state).
type modelSnapshot struct {
	items        []ContentItem
	pendingItems []ContentItem
}

func (s modelSnapshot) hasActiveQA() bool {
	for _, item := range s.items {
		if item.Type == "qa" {
			return true
		}
	}
	return false
}

// Run starts the interactive HUD.
func Run(cfg *config.Config) error {
	client := NewIPCClient(cfg)
	if !client.IsRunning() {
		return fmt.Errorf("clara daemon is not running. Run 'clara serve' first")
	}

	theme := DefaultTheme()
	content := newContentModel(theme)
	prompt := newPromptModel(theme)

	m := &appModel{
		cfg:     cfg,
		client:  client,
		theme:   theme,
		content: content,
		prompt:  prompt,
		msgChan: make(chan tea.Msg, 100),
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

type notificationMsg struct {
	ID       int64
	RunID    string
	IntentID string
	Message  string
}

type interactiveNotificationMsg struct {
	ID           int64
	RunID        string
	IntentID     string
	Prompt       string
	Options      []string
	ResponseChan chan string
}

type historyLoadedMsg struct {
	Items []map[string]any
}

type appModel struct {
	cfg           *config.Config
	client        *IPCClient
	theme         *Theme
	msgChan       chan tea.Msg
	snapshot      atomic.Value // stores modelSnapshot; updated after every Update call
	HistoryLoaded bool         // Used by tests to know when history has been fetched

	width  int
	height int

	content *contentModel
	prompt  *promptModel
}

func (m *appModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.prompt.Init(),
		m.content.Init(),
		m.loadHistoryCmd(),
	)
}

func (m *appModel) loadHistoryCmd() tea.Cmd {
	return func() tea.Msg {
		history, err := m.client.LoadTUIHistory(100)
		if err != nil {
			return notificationMsg{Message: fmt.Sprintf("Failed to load history: %v", err)}
		}
		return historyLoadedMsg{Items: history}
	}
}

func (m *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.storeSnapshot()
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD {
			return m, tea.Quit
		}

		// Intercept 1-9 for interactive selection if content model is active
		if m.content.hasActiveQA() && msg.Type == tea.KeyRunes {
			if len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9' {
				answer, item := m.content.answerQA(int(msg.Runes[0] - '0'))
				if answer != "" && item.ResponseChan == nil {
					// Offline prompt - update DB and resume
					go func() {
						_ = m.client.UpdateTUIAnswer(item.ID, item.IntentID, answer, true)
					}()
				}
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		promptHeight := 2 // single line + border padding
		contentHeight := max(m.height-promptHeight, 0)

		m.prompt.SetSize(m.width, promptHeight)
		m.content.SetSize(m.width, contentHeight)

	case historyLoadedMsg:
		m.HistoryLoaded = true
		// Save newly arrived items (e.g. from notification system) to re-add after history
		newlyArrived := m.content.items
		// Remove the placeholder if it's the only thing there
		if len(newlyArrived) == 1 && newlyArrived[0].ID == 0 && strings.Contains(newlyArrived[0].Text, "System online") {
			newlyArrived = nil
		}

		// Reset items to populate from history
		m.content.items = nil
		m.content.pendingItems = nil

		for _, item := range msg.Items {
			id, _ := item["ID"].(float64)
			runID, _ := item["RunID"].(string)
			intentID, _ := item["IntentID"].(string)
			kind, _ := item["Kind"].(string)
			text, _ := item["Text"].(string)
			answer, _ := item["Answer"].(string)
			switch kind {
			case "notification":
				if answer != "" {
					text = fmt.Sprintf("%s\nAnswered: %s", text, answer)
				}
				m.content.addItem(ContentItem{
					ID:       int64(id),
					RunID:    runID,
					IntentID: intentID,
					Type:     "notification",
					Text:     text,
				})
			case "qa":
				if answer != "" {
					m.content.addItem(ContentItem{
						ID:       int64(id),
						RunID:    runID,
						IntentID: intentID,
						Type:     "notification",
						Text:     fmt.Sprintf("%s\nAnswered: %s", text, answer),
					})
				} else {
					var opts []string
					if rawOpts, ok := item["Options"].([]any); ok {
						for _, ro := range rawOpts {
							if s, ok := ro.(string); ok {
								opts = append(opts, s)
							}
						}
					}
					m.content.addItem(ContentItem{
						ID:       int64(id),
						RunID:    runID,
						IntentID: intentID,
						Type:     "qa",
						Text:     text,
						Options:  opts,
					})
				}
			}
		}

		// Re-add newly arrived items (maintaining order: history first, then new items)
		for _, item := range newlyArrived {
			m.content.addItem(item)
		}

		return m, nil

	case notificationMsg:
		m.content.addItem(ContentItem{
			ID:       msg.ID,
			RunID:    msg.RunID,
			IntentID: msg.IntentID,
			Type:     "notification",
			Text:     msg.Message,
		})
		return m, nil

	case interactiveNotificationMsg:
		m.content.addItem(ContentItem{
			ID:           msg.ID,
			RunID:        msg.RunID,
			IntentID:     msg.IntentID,
			Type:         "qa",
			Text:         msg.Prompt,
			Options:      msg.Options,
			ResponseChan: msg.ResponseChan,
		})
		return m, nil
	}

	newContent, cmdContent := m.content.Update(msg)
	m.content = newContent.(*contentModel)
	cmds = append(cmds, cmdContent)

	newPrompt, cmdPrompt := m.prompt.Update(msg)
	m.prompt = newPrompt.(*promptModel)
	cmds = append(cmds, cmdPrompt)

	return m, tea.Batch(cmds...)
}

func (m *appModel) storeSnapshot() {
	m.snapshot.Store(modelSnapshot{
		items:        append([]ContentItem(nil), m.content.items...),
		pendingItems: append([]ContentItem(nil), m.content.pendingItems...),
	})
}

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
