package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type promptModel struct {
	theme *Theme
	input textarea.Model

	width  int
	height int
}

func newPromptModel(theme *Theme) *promptModel {
	ta := textarea.New()
	ta.Placeholder = "Type a question or pick an option (1-9)..."
	ta.Focus()

	ta.Prompt = "❯ "
	ta.CharLimit = 1000
	ta.ShowLineNumbers = false

	// Apply styles
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(theme.Text)
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(theme.Dim)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.LineNumber = lipgloss.NewStyle()
	ta.FocusedStyle.EndOfBuffer = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(theme.Highlight)

	// Apply styles to blurred as well
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Text = lipgloss.NewStyle().Foreground(theme.Text)
	ta.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(theme.Dim)
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.LineNumber = lipgloss.NewStyle()
	ta.BlurredStyle.EndOfBuffer = lipgloss.NewStyle()
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(theme.Highlight)

	ta.Cursor.Style = lipgloss.NewStyle().Foreground(theme.Highlight)
	ta.Cursor.Blink = false // Force blink off initially

	return &promptModel{
		theme: theme,
		input: ta,
	}
}

func (m *promptModel) Init() tea.Cmd {
	return nil
}

func (m *promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	m.input, cmd = m.input.Update(msg)

	// Force blink off explicitly after every update to prevent it from
	// restarting when typing.
	m.input.Cursor.Blink = false

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlL:
			m.input.Reset()
			return m, nil
		}
	}

	return m, cmd
}

func (m *promptModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Remove horizontal overhead calculation if using full width
	// Padding (2) + Prompt (2) = 4
	m.input.SetWidth(m.width - 4)
	m.input.SetHeight(m.height - 1) // Leave 1 line for the top border
}

func (m *promptModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// Trim trailing newline from textarea to prevent it from breaking the
	// background fill in the lipgloss container.
	view := strings.TrimRight(m.input.View(), "\n")

	// The container style width should be m.width - 1 (to account for the border).
	// Lipgloss Width() includes padding.
	// Removed fixed Height(1) to prevent clipping.
	style := m.theme.PromptStyle.
		Width(m.width - 1)

	return style.Render(view)
}
