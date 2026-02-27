package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- Styles -----------------------------------------------------------------

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	approvedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).Bold(true)

	rejectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).Bold(true)

	similarityStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	contextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true)
)

// ---- List item --------------------------------------------------------------

type suggestionItem struct {
	suggestion Suggestion
}

func (i suggestionItem) Title() string {
	src := filepath.Base(i.suggestion.SourcePath)
	src = strings.TrimSuffix(src, filepath.Ext(src))
	return fmt.Sprintf("%s  →  [[%s]]", src, i.suggestion.TargetTitle)
}

func (i suggestionItem) Description() string {
	sim := similarityStyle.Render(fmt.Sprintf("%.0f%% match", i.suggestion.Similarity*100))
	ctx := contextStyle.Render(truncate(i.suggestion.Context, 80))
	return fmt.Sprintf("%s  %s", sim, ctx)
}

func (i suggestionItem) FilterValue() string {
	return i.suggestion.SourcePath + i.suggestion.TargetTitle
}

// ---- Messages ---------------------------------------------------------------

type suggestionsLoadedMsg struct{ items []Suggestion }
type errMsg struct{ err error }
type actionDoneMsg struct{ id int64; approved bool }

// ---- Model ------------------------------------------------------------------

type Model struct {
	api     *APIClient
	list    list.Model
	spinner spinner.Model
	loading bool
	status  string
	width   int
	height  int
}

func New(apiClient *APIClient) Model {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("205")).
		BorderLeftForeground(lipgloss.Color("205"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Clara — Backlink Suggestions"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = titleStyle

	s := spinner.New()
	s.Spinner = spinner.Dot

	return Model{
		api:     apiClient,
		list:    l,
		spinner: s,
		loading: true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.loadSuggestions())
}

func (m Model) loadSuggestions() tea.Cmd {
	return func() tea.Msg {
		items, err := m.api.ListSuggestions(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return suggestionsLoadedMsg{items}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(msg.Width, msg.Height-4)

	case tea.KeyMsg:
		if m.loading {
			break
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "y":
			if item, ok := m.list.SelectedItem().(suggestionItem); ok {
				id := item.suggestion.ID
				return m, m.actOnSuggestion(id, true)
			}

		case "n":
			if item, ok := m.list.SelectedItem().(suggestionItem); ok {
				id := item.suggestion.ID
				return m, m.actOnSuggestion(id, false)
			}

		case "r":
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.loadSuggestions())
		}

	case suggestionsLoadedMsg:
		m.loading = false
		items := make([]list.Item, len(msg.items))
		for i, s := range msg.items {
			items[i] = suggestionItem{s}
		}
		m.list.SetItems(items)
		if len(items) == 0 {
			m.status = "No pending suggestions."
		} else {
			m.status = ""
		}

	case actionDoneMsg:
		// Remove the acted-on item from the list
		items := m.list.Items()
		newItems := make([]list.Item, 0, len(items))
		for _, item := range items {
			if si, ok := item.(suggestionItem); ok && si.suggestion.ID != msg.id {
				newItems = append(newItems, item)
			}
		}
		m.list.SetItems(newItems)
		if msg.approved {
			m.status = approvedStyle.Render("✓ Approved — agent will apply link")
		} else {
			m.status = rejectedStyle.Render("✗ Rejected")
		}

	case errMsg:
		m.loading = false
		m.status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(
			"Error: " + msg.err.Error(),
		)

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	if !m.loading {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) actOnSuggestion(id int64, approve bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if approve {
			err = m.api.Approve(context.Background(), id)
		} else {
			err = m.api.Reject(context.Background(), id)
		}
		if err != nil {
			return errMsg{err}
		}
		return actionDoneMsg{id: id, approved: approve}
	}
}

func (m Model) View() string {
	if m.loading {
		return fmt.Sprintf("\n  %s Loading suggestions…\n\n%s",
			m.spinner.View(),
			helpStyle.Render("  ctrl+c to quit"),
		)
	}

	var sb strings.Builder
	sb.WriteString(m.list.View())

	if m.status != "" {
		sb.WriteString("\n  " + m.status)
	}

	sb.WriteString("\n" + helpStyle.Render("  y approve  ·  n reject  ·  r refresh  ·  q quit"))

	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
