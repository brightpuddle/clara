package cockpit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	headerSubStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("246")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	gitBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("40")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	stateStyleRunning = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	stateStyleIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	stateStyleBlocked = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
)

// SessionClient defines the Clara operations required by the interactive cockpit.
type SessionClient interface {
	GetSession(ctx context.Context, id string) (SessionSummary, error)
	ReadEvents(ctx context.Context, session string, after int64) ([]EventItem, error)
	SendPrompt(ctx context.Context, session, prompt string) error
	Interrupt(ctx context.Context, session string) error
	SetModel(ctx context.Context, session, model string) error
}

// Model is the Bubble Tea model for the interactive attach session.
type Model struct {
	client       SessionClient
	session      SessionSummary
	git          GitInfo
	viewport     viewport.Model
	textInput    textinput.Model
	spinner      spinner.Model
	transcript   strings.Builder
	lastSeq      int64
	width        int
	height       int
	ready        bool
	err          error
	quitting     bool
	editorActive bool
}

type eventsMsg []EventItem
type sessionMsg SessionSummary
type errMsg error
type promptSentMsg struct{}
type editorDoneMsg string

// NewAttachModel creates a new interactive session cockpit Model.
func NewAttachModel(client SessionClient, sess SessionSummary) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a prompt, /model, /interrupt, or Ctrl-G for $EDITOR..."
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 80

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	git := DetectGitStatus(sess.Dir)

	return Model{
		client:    client,
		session:   sess,
		git:       git,
		textInput: ti,
		spinner:   sp,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.spinner.Tick,
		m.pollEventsCmd(),
		m.pollSessionCmd(),
	)
}

func (m Model) pollEventsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		events, err := m.client.ReadEvents(ctx, m.session.ID, m.lastSeq)
		if err != nil {
			return errMsg(err)
		}
		return eventsMsg(events)
	}
}

func (m Model) pollSessionCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s, err := m.client.GetSession(ctx, m.session.ID)
		if err != nil {
			return errMsg(err)
		}
		return sessionMsg(s)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
		cmds  []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit

		case tea.KeyCtrlG:
			// Launch external $EDITOR (Neovim)
			return m, m.openEditorCmd()

		case tea.KeyEnter:
			input := strings.TrimSpace(m.textInput.Value())
			if input == "" {
				break
			}
			m.textInput.SetValue("")

			isSlash, cmd, args := ParseSlashCommand(input)
			if isSlash {
				return m, m.handleSlashCmd(cmd, args)
			}

			// Send normal prompt
			return m, m.sendPromptCmd(input)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerHeight := 3
		inputHeight := 3
		vpHeight := msg.Height - headerHeight - inputHeight
		if vpHeight < 5 {
			vpHeight = 5
		}

		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.viewport.SetContent(m.transcript.String())
			m.viewport.GotoBottom()
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}
		m.textInput.Width = msg.Width - 4

	case eventsMsg:
		if len(msg) > 0 {
			for _, e := range msg {
				if e.HostSeq > m.lastSeq {
					m.lastSeq = e.HostSeq
				}
				rendered := RenderEvent(e)
				if rendered != "" {
					m.transcript.WriteString(rendered)
				}
			}
			m.viewport.SetContent(m.transcript.String())
			m.viewport.GotoBottom()
		}
		// Continue polling for events every 500ms
		cmds = append(cmds, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return m.pollEventsCmd()()
		}))

	case sessionMsg:
		m.session = SessionSummary(msg)
		// Poll session state every 2 seconds
		cmds = append(cmds, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return m.pollSessionCmd()()
		}))

	case editorDoneMsg:
		text := strings.TrimSpace(string(msg))
		if text != "" {
			return m, m.sendPromptCmd(text)
		}

	case promptSentMsg:
		cmds = append(cmds, m.pollEventsCmd())

	case errMsg:
		m.err = msg
	}

	m.textInput, tiCmd = m.textInput.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	m.spinner, spCmd = m.spinner.Update(msg)
	cmds = append(cmds, tiCmd, vpCmd, spCmd)

	return m, tea.Batch(cmds...)
}

func (m Model) openEditorCmd() tea.Cmd {
	return func() tea.Msg {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "nvim"
		}

		tmpFile, err := os.CreateTemp("", "clara-prompt-*.md")
		if err != nil {
			return errMsg(err)
		}
		tmpPath := tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(tmpPath)

		c := exec.Command(editor, tmpPath)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return errMsg(err)
		}

		content, err := os.ReadFile(tmpPath)
		if err != nil {
			return errMsg(err)
		}
		return editorDoneMsg(string(content))
	}
}

func (m Model) sendPromptCmd(prompt string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.client.SendPrompt(ctx, m.session.ID, prompt); err != nil {
			return errMsg(err)
		}
		return promptSentMsg{}
	}
}

func (m Model) handleSlashCmd(cmd, args string) tea.Cmd {
	switch cmd {
	case "edit":
		return m.openEditorCmd()

	case "interrupt", "cancel":
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := m.client.Interrupt(ctx, m.session.ID); err != nil {
				return errMsg(err)
			}
			return m.pollEventsCmd()()
		}

	case "model":
		if args == "" {
			m.transcript.WriteString(statusStyle.Render(fmt.Sprintf("• Current model: %s\n", m.session.Model)))
			m.viewport.SetContent(m.transcript.String())
			m.viewport.GotoBottom()
			return nil
		}
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := m.client.SetModel(ctx, m.session.ID, args); err != nil {
				return errMsg(err)
			}
			m.session.Model = args
			return m.pollSessionCmd()()
		}

	case "detach", "quit":
		m.quitting = true
		return tea.Quit

	default:
		m.transcript.WriteString(errorStyle.Render(fmt.Sprintf("Unknown command /%s. Available: /edit, /model, /interrupt, /cancel, /detach\n", cmd)))
		m.viewport.SetContent(m.transcript.String())
		m.viewport.GotoBottom()
		return nil
	}
}

func (m Model) View() string {
	if m.quitting {
		return "Detached from session.\n"
	}

	// 1. Header
	stateBadge := m.session.State
	switch m.session.State {
	case "running":
		stateBadge = stateStyleRunning.Render(fmt.Sprintf("%s running", m.spinner.View()))
	case "idle":
		stateBadge = stateStyleIdle.Render("idle")
	case "blocked":
		stateBadge = stateStyleBlocked.Render("blocked")
	}

	title := m.session.Title
	if title == "" {
		title = m.session.ID
	}

	topLine := headerStyle.Render(fmt.Sprintf(" CLARA COCKPIT • %s (%s) ", title, m.session.Agent))
	if m.git.IsRepo {
		topLine += gitBadgeStyle.Render(fmt.Sprintf("  %s ", m.git.Format()))
	}
	topLine += "\n"

	metaLine := headerSubStyle.Render(fmt.Sprintf("Host: %s | Model: %s | Queue: %d | State: %s",
		m.session.Host, m.session.Model, m.session.QueueLen, stateBadge)) + "\n\n"

	// 2. Viewport
	content := m.viewport.View() + "\n\n"

	// 3. Input bar
	inputLine := m.textInput.View()

	if m.err != nil {
		inputLine += "\n" + errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}

	return topLine + metaLine + content + inputLine
}
