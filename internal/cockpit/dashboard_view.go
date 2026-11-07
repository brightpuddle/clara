package cockpit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	headerTableStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("250")).
				Background(lipgloss.Color("238")).
				Padding(0, 1)

	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("24")).
				Padding(0, 1)

	normalRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(0, 1)
)

// DashboardClient defines operations needed by the cockpit dashboard.
type DashboardClient interface {
	SessionClient
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	CloseSession(ctx context.Context, id string) error
	CreateSession(ctx context.Context, agent, dir, prompt string) (string, error)
}

type sessionListMsg []SessionSummary
type closeSessionDoneMsg string

// DashboardModel is the top-level Bubble Tea model for the cockpit dashboard.
type DashboardModel struct {
	client       DashboardClient
	sessions     []SessionSummary
	filtered     []SessionSummary
	cursor       int
	filterInput  textinput.Model
	filtering    bool
	width        int
	height       int
	spinner      spinner.Model
	err          error
	quitting     bool

	// Active attached child session (if Enter was pressed)
	attachedMode bool
	attachModel  Model
}

// NewDashboardModel initializes the Cockpit Dashboard.
func NewDashboardModel(client DashboardClient) DashboardModel {
	ti := textinput.New()
	ti.Placeholder = "Type to filter sessions..."
	ti.Prompt = "/ "
	ti.CharLimit = 64

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return DashboardModel{
		client:      client,
		filterInput: ti,
		spinner:     sp,
	}
}

func (m DashboardModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.fetchSessionsCmd(),
	)
}

func (m DashboardModel) fetchSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		sessions, err := m.client.ListSessions(ctx)
		if err != nil {
			return errMsg(err)
		}
		return sessionListMsg(sessions)
	}
}

func (m DashboardModel) closeSessionCmd(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.client.CloseSession(ctx, id)
		return closeSessionDoneMsg(id)
	}
}

func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If currently attached into a session full-screen, forward to attachModel
	if m.attachedMode {
		var cmd tea.Cmd
		updatedAttach, cmd := m.attachModel.Update(msg)
		m.attachModel = updatedAttach.(Model)
		if m.attachModel.quitting {
			m.attachedMode = false
			return m, m.fetchSessionsCmd()
		}
		return m, cmd
	}

	var (
		cmds []tea.Cmd
		tiCmd tea.Cmd
		spCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.filtering {
			switch msg.Type {
			case tea.KeyEsc, tea.KeyEnter:
				m.filtering = false
				m.filterInput.Blur()
			default:
				m.filterInput, tiCmd = m.filterInput.Update(msg)
				m.filtered = FilterSessions(m.sessions, m.filterInput.Value())
				if m.cursor >= len(m.filtered) {
					m.cursor = max(0, len(m.filtered)-1)
				}
				return m, tiCmd
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "/":
			m.filtering = true
			m.filterInput.Focus()
			return m, textinput.Blink

		case "j", "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}

		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}

		case "x":
			// Close selected session
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				target := m.filtered[m.cursor]
				return m, m.closeSessionCmd(target.ID)
			}

		case "enter":
			// Open selected session full-screen in attach client
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				target := m.filtered[m.cursor]
				m.attachedMode = true
				m.attachModel = NewAttachModel(m.client, target)
				return m, m.attachModel.Init()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.attachedMode {
			var cmd tea.Cmd
			updatedAttach, cmd := m.attachModel.Update(msg)
			m.attachModel = updatedAttach.(Model)
			return m, cmd
		}

	case sessionListMsg:
		m.sessions = SortSessions([]SessionSummary(msg))
		m.filtered = FilterSessions(m.sessions, m.filterInput.Value())
		if m.cursor >= len(m.filtered) {
			m.cursor = max(0, len(m.filtered)-1)
		}
		// Periodically refresh sessions every 3 seconds
		cmds = append(cmds, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
			return m.fetchSessionsCmd()()
		}))

	case closeSessionDoneMsg:
		cmds = append(cmds, m.fetchSessionsCmd())

	case errMsg:
		m.err = msg
	}

	m.spinner, spCmd = m.spinner.Update(msg)
	cmds = append(cmds, spCmd)

	return m, tea.Batch(cmds...)
}

func (m DashboardModel) View() string {
	if m.quitting {
		return "Exited Clara Cockpit.\n"
	}
	if m.attachedMode {
		return m.attachModel.View()
	}

	// 1. Title bar
	titleBar := titleStyle.Render(" CLARA COCKPIT • AGENT SESSIONS ")
	statusShort := FormatStatusShort(m.sessions)
	titleBar += "  " + statusStyle.Render(fmt.Sprintf("[%s]", statusShort)) + "\n\n"

	// 2. Filter input (if active or has text)
	var filterBar string
	if m.filtering || m.filterInput.Value() != "" {
		filterBar = m.filterInput.View() + "\n\n"
	}

	// 3. Table header
	header := headerTableStyle.Render(fmt.Sprintf("%-2s %-12s %-8s %-10s %-22s %-25s %s",
		"", "HOST", "AGENT", "STATE", "MODEL", "DIRECTORY", "TITLE")) + "\n"

	// 4. Session rows
	var rows strings.Builder
	if len(m.filtered) == 0 {
		rows.WriteString("   No agent sessions found.\n")
	} else {
		for i, s := range m.filtered {
			pointer := "  "
			if i == m.cursor {
				pointer = "❯ "
			}

			stateBadge := s.State
			switch s.State {
			case "running":
				stateBadge = stateStyleRunning.Render("running " + m.spinner.View())
			case "idle":
				stateBadge = stateStyleIdle.Render("idle")
			case "blocked", "failed":
				stateBadge = stateStyleBlocked.Render(s.State)
			}

			dirDisplay := s.Dir
			if len(dirDisplay) > 24 {
				parts := strings.Split(dirDisplay, "/")
				if len(parts) > 2 {
					dirDisplay = ".../" + parts[len(parts)-2] + "/" + parts[len(parts)-1]
				}
			}

			titleDisplay := s.Title
			if titleDisplay == "" {
				titleDisplay = s.ID
			}
			if len(titleDisplay) > 35 {
				titleDisplay = titleDisplay[:32] + "..."
			}

			modelDisplay := s.Model
			if modelDisplay == "" {
				modelDisplay = "-"
			}
			if len(modelDisplay) > 20 {
				modelDisplay = modelDisplay[:18] + ".."
			}

			line := fmt.Sprintf("%-2s %-12s %-8s %-10s %-22s %-25s %s",
				pointer, s.Host, s.Agent, stateBadge, modelDisplay, dirDisplay, titleDisplay)

			if i == m.cursor {
				rows.WriteString(selectedRowStyle.Render(line) + "\n")
			} else {
				rows.WriteString(normalRowStyle.Render(line) + "\n")
			}
		}
	}

	// 5. Footer help
	footer := "\n" + footerStyle.Render("Keys: [j/k] Navigate • [Enter] Attach • [/] Filter • [x] Close • [q] Quit")

	if m.err != nil {
		footer += "\n" + errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}

	return titleBar + filterBar + header + rows.String() + footer
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
