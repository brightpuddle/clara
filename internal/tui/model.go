package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/ipc"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Messages
type snapshotMsg struct {
	snap *DaemonSnapshot
	err  error
}

type streamEntryMsg struct {
	entry ipc.StreamEntry
}

type tickMsg time.Time

// Model is the main Bubble Tea model for the Clara lazygit-style TUI.
type Model struct {
	socketPath string
	cfgPath    string

	state       *AppState
	activePanel Panel
	width       int
	height      int

	online    bool
	statusMsg string

	showHelp   bool
	filterMode bool

	streamChan chan ipc.StreamEntry
	cancelFunc context.CancelFunc
}

// NewModel creates an initialized TUI Model.
func NewModel(socketPath, cfgPath string) Model {
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	return Model{
		socketPath:  socketPath,
		cfgPath:     cfgPath,
		state:       NewAppState(),
		activePanel: PanelEvents,
		streamChan:  make(chan ipc.StreamEntry, 100),
	}
}

// Init starts initial data fetching and streaming goroutines.
func (m Model) Init() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel

	// Start stream readers
	go StreamLogs(ctx, m.socketPath, ipc.MethodEventLogs, 50, m.streamChan)
	go StreamLogs(ctx, m.socketPath, ipc.MethodEvaluatorLogs, 50, m.streamChan)
	go StreamLogs(ctx, m.socketPath, ipc.MethodActuatorLogs, 50, m.streamChan)

	return tea.Batch(
		m.fetchSnapshotCmd(),
		m.waitForStreamCmd(),
		m.tickCmd(),
	)
}

func (m Model) fetchSnapshotCmd() tea.Cmd {
	return func() tea.Msg {
		snap, err := FetchSnapshot(m.socketPath)
		return snapshotMsg{snap: snap, err: err}
	}
}

func (m Model) waitForStreamCmd() tea.Cmd {
	return func() tea.Msg {
		entry, ok := <-m.streamChan
		if !ok {
			return nil
		}
		return streamEntryMsg{entry: entry}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update processes Bubble Tea messages and keyboard input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case snapshotMsg:
		if msg.snap != nil {
			m.online = msg.snap.Online
			m.statusMsg = msg.snap.StatusMsg

			// Merge automations into rules
			rules := make([]RuleItem, 0, len(msg.snap.Automations))
			for _, aut := range msg.snap.Automations {
				rules = append(rules, RuleItem{
					ID:           aut.RuleID,
					Name:         aut.Name,
					ActuatorID:   aut.ActuatorID,
					Routing:      aut.Routing,
					TTL:          aut.TTL,
					ExpiresIn:    aut.ExpiresIn,
					Description:  aut.Description,
					Triggers:     aut.Triggers,
					Capabilities: aut.Capabilities,
				})
			}
			m.state.Rules = rules

			// Merge actuators
			actuators := make([]ActuatorItem, 0, len(msg.snap.Actuators))
			for _, a := range msg.snap.Actuators {
				actuators = append(actuators, ActuatorItem{
					ID:          a.ID,
					Description: a.Description,
					Status:      a.Status,
				})
			}
			m.state.Actuators = actuators

			// Merge approvals
			approvals := make([]ApprovalItem, 0, len(msg.snap.Approvals))
			for _, app := range msg.snap.Approvals {
				opts := make([]ResolutionOption, 0, len(app.Options))
				for _, o := range app.Options {
					opts = append(opts, ResolutionOption{
						ID:          o.ID,
						Description: o.Description,
						ActionCode:  o.ActionCode,
					})
				}
				approvals = append(approvals, ApprovalItem{
					RequestID: app.RequestID,
					Context:   app.Context,
					CreatedAt: time.Now(),
					Options:   opts,
				})
			}
			m.state.Approvals = approvals
			m.state.Tools = msg.snap.Tools
		}
		return m, nil

	case streamEntryMsg:
		entry := msg.entry
		switch entry.Stream {
		case "event":
			dataMap, _ := entry.Data.(map[string]any)
			evTime := time.Now()
			if entry.Time != "" {
				if t, err := time.Parse(time.RFC3339, entry.Time); err == nil {
					evTime = t
				}
			}
			m.state.AddOrUpdateEvent(EventItem{
				ID:      entry.ID,
				Time:    evTime,
				Type:    entry.Type,
				Source:  entry.Source,
				Status:  "pending",
				Data:    dataMap,
				RawJSON: PrettyJSON(dataMap),
			})

		case "evaluator":
			fields, _ := entry.Data.(map[string]any)
			eventID, _ := fields["event_id"].(string)
			actuatorID, _ := fields["actuator"].(string)
			ruleID, _ := fields["rule_id"].(string)

			status := "pending"
			routing := "fast-path"
			if entry.Msg == "fast-path heuristic hit" {
				routing = "fast-path"
			} else if strings.HasPrefix(entry.Msg, "LLM:") {
				routing = "llm-dynamic"
			}

			if entry.Level == "error" {
				status = "error"
			}

			logText := fmt.Sprintf("[%s] Evaluator: %s", entry.Level, entry.Msg)
			if eventID != "" {
				m.state.AddOrUpdateEvent(EventItem{
					ID:         eventID,
					ActuatorID: actuatorID,
					RuleID:     ruleID,
					Routing:    routing,
					Status:     status,
					Logs:       []string{logText},
				})
			} else {
				m.state.AppendEventLog("", logText)
			}

		case "actuator":
			success := entry.Level != "warn" && entry.Level != "error"
			outStr := ""
			errStr := ""
			eventID := ""
			if dataMap, ok := entry.Data.(map[string]any); ok {
				if out, ok := dataMap["output"].(string); ok {
					outStr = out
				}
				if ev, ok := dataMap["event_id"].(string); ok {
					eventID = ev
				}
			}
			if !success {
				errStr = entry.Msg
			}
			m.state.RecordActuatorRun(entry.ActuatorID, success, outStr, errStr)

			logText := fmt.Sprintf("[%s] Actuator (%s): %s", entry.Level, entry.ActuatorID, entry.Msg)
			if eventID != "" {
				status := "success"
				if !success {
					status = "error"
				}
				m.state.AddOrUpdateEvent(EventItem{
					ID:         eventID,
					ActuatorID: entry.ActuatorID,
					Status:     status,
					Logs:       []string{logText},
				})
			} else {
				m.state.AppendEventLog("", logText)
			}
		}
		return m, m.waitForStreamCmd()

	case tickMsg:
		return m, tea.Batch(m.fetchSnapshotCmd(), m.tickCmd())

	case EditorFinishedMsg:
		return m, m.fetchSnapshotCmd()

	case tea.KeyMsg:
		if m.showHelp {
			if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
				m.showHelp = false
			}
			return m, nil
		}

		if m.filterMode {
			switch msg.String() {
			case "esc", "enter":
				m.filterMode = false
			case "backspace":
				if len(m.state.FilterText) > 0 {
					m.state.FilterText = m.state.FilterText[:len(m.state.FilterText)-1]
				}
			default:
				if len(msg.String()) == 1 {
					m.state.FilterText += msg.String()
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			if m.cancelFunc != nil {
				m.cancelFunc()
			}
			return m, tea.Quit

		case "?":
			m.showHelp = true
			return m, nil

		case "/":
			m.filterMode = true
			return m, nil

		case "esc":
			m.state.FilterText = ""
			return m, nil

		case "r":
			return m, m.fetchSnapshotCmd()

		// Panel switching hotkeys (1-4, tab, [, ])
		case "1":
			m.activePanel = PanelEvents
		case "2":
			m.activePanel = PanelRules
		case "3":
			m.activePanel = PanelActuators
		case "4":
			m.activePanel = PanelApprovals
		case "5":
			m.activePanel = PanelInspector
		case "tab", "]":
			m.activePanel = (m.activePanel + 1) % 5
		case "shift+tab", "[":
			m.activePanel = (m.activePanel + 4) % 5

		// Item navigation (j/k, down/up, g/G)
		case "j", "down":
			m.moveSelection(1)
		case "k", "up":
			m.moveSelection(-1)
		case "g":
			m.jumpSelection(0)
		case "G":
			m.jumpSelection(-1)

		// Actions
		case " ", "enter":
			if m.activePanel == PanelApprovals && len(m.state.Approvals) > 0 && m.state.ApprovalIdx < len(m.state.Approvals) {
				reqID := m.state.Approvals[m.state.ApprovalIdx].RequestID
				_ = DecideApproval(m.socketPath, reqID, 1) // Option 1: Allow
				return m, m.fetchSnapshotCmd()
			} else if m.activePanel != PanelInspector {
				m.activePanel = PanelInspector
			} else {
				m.activePanel = PanelEvents
			}

		case "d":
			if m.activePanel == PanelApprovals && len(m.state.Approvals) > 0 && m.state.ApprovalIdx < len(m.state.Approvals) {
				reqID := m.state.Approvals[m.state.ApprovalIdx].RequestID
				_ = DecideApproval(m.socketPath, reqID, 2) // Option 2: Deny
				return m, m.fetchSnapshotCmd()
			}

		case "e":
			// Open config file or rule in $EDITOR
			targetPath := m.cfgPath
			if _, err := os.Stat(targetPath); err != nil {
				// Fallback to creating a sample rule definition
				targetPath = filepath.Join(os.TempDir(), "clara-rules.yaml")
			}
			return m, OpenInEditor(targetPath)
		}
	}

	return m, nil
}

func (m *Model) moveSelection(delta int) {
	switch m.activePanel {
	case PanelEvents, PanelInspector:
		events := m.state.FilteredEvents()
		if len(events) == 0 {
			return
		}
		m.state.EventIdx = clamp(m.state.EventIdx+delta, 0, len(events)-1)
	case PanelRules:
		rules := m.state.FilteredRules()
		if len(rules) == 0 {
			return
		}
		m.state.RuleIdx = clamp(m.state.RuleIdx+delta, 0, len(rules)-1)
	case PanelActuators:
		actuators := m.state.FilteredActuators()
		if len(actuators) == 0 {
			return
		}
		m.state.ActuatorIdx = clamp(m.state.ActuatorIdx+delta, 0, len(actuators)-1)
	case PanelApprovals:
		if len(m.state.Approvals) == 0 {
			return
		}
		m.state.ApprovalIdx = clamp(m.state.ApprovalIdx+delta, 0, len(m.state.Approvals)-1)
	}
}

func (m *Model) jumpSelection(pos int) {
	switch m.activePanel {
	case PanelEvents, PanelInspector:
		events := m.state.FilteredEvents()
		if len(events) == 0 {
			return
		}
		if pos == -1 {
			m.state.EventIdx = len(events) - 1
		} else {
			m.state.EventIdx = 0
		}
	case PanelRules:
		rules := m.state.FilteredRules()
		if len(rules) == 0 {
			return
		}
		if pos == -1 {
			m.state.RuleIdx = len(rules) - 1
		} else {
			m.state.RuleIdx = 0
		}
	case PanelActuators:
		actuators := m.state.FilteredActuators()
		if len(actuators) == 0 {
			return
		}
		if pos == -1 {
			m.state.ActuatorIdx = len(actuators) - 1
		} else {
			m.state.ActuatorIdx = 0
		}
	case PanelApprovals:
		if len(m.state.Approvals) == 0 {
			return
		}
		if pos == -1 {
			m.state.ApprovalIdx = len(m.state.Approvals) - 1
		} else {
			m.state.ApprovalIdx = 0
		}
	}
}

// View renders the TUI layout.
func (m Model) View() string {
	if m.width < 40 || m.height < 10 {
		return "Terminal window too small for Clara TUI."
	}

	renderer := NewViewRenderer(m.state, m.width, m.height)

	// Calculate 2-column dimensions
	usableHeight := m.height - 2 // space for status bar at bottom
	if usableHeight < 8 {
		usableHeight = 8
	}

	leftWidth := m.width / 3
	if leftWidth < 28 {
		leftWidth = 28
	}
	if leftWidth > 45 {
		leftWidth = 45
	}
	rightWidth := m.width - leftWidth - 1

	// Left column has 4 stacked panels: Events (35%), Rules (25%), Actuators (25%), Approvals (15%)
	p1Height := int(float64(usableHeight) * 0.35)
	p2Height := int(float64(usableHeight) * 0.25)
	p3Height := int(float64(usableHeight) * 0.25)
	p4Height := usableHeight - p1Height - p2Height - p3Height
	if p4Height < 3 {
		p4Height = 3
	}

	p1 := renderer.RenderEventsPanel(m.activePanel == PanelEvents, leftWidth, p1Height)
	p2 := renderer.RenderRulesPanel(m.activePanel == PanelRules, leftWidth, p2Height)
	p3 := renderer.RenderActuatorsPanel(m.activePanel == PanelActuators, leftWidth, p3Height)
	p4 := renderer.RenderApprovalsPanel(m.activePanel == PanelApprovals, leftWidth, p4Height)

	leftCol := lipgloss.JoinVertical(lipgloss.Left, p1, p2, p3, p4)
	rightCol := renderer.RenderInspectorPanel(m.activePanel, rightWidth, usableHeight)

	mainBody := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol)

	statusBar := renderer.RenderStatusBar(m.online, m.statusMsg)
	if m.filterMode {
		filterLine := StyleCyan.Render(" Filter: ") + m.state.FilterText + StyleDim.Render(" (press enter to apply, esc to clear)")
		statusBar = filterLine
	}

	out := lipgloss.JoinVertical(lipgloss.Left, mainBody, statusBar)

	if m.showHelp {
		// Overlay help modal in center
		modal := renderer.RenderHelpModal()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	return out
}

func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
