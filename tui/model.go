// Package tui implements the Clara terminal user interface using Bubbletea.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rs/zerolog"

	agentv1 "github.com/brightpuddle/clara/gen/agent/v1"
	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	tuigrpc "github.com/brightpuddle/clara/tui/grpc"
	"github.com/brightpuddle/clara/tui/panes"
	"github.com/brightpuddle/clara/tui/styles"
)

// focusedPane enumerates the pane that currently has keyboard focus.
type focusedPane int

const (
	paneArtifacts focusedPane = iota
	paneRelated
	paneDetail
)

// PaneCount is the total number of focusable panes.
const PaneCount = 3

// Msg types.

type artifactsLoadedMsg struct {
	artifacts []*artifactv1.Artifact
}

type artifactDetailMsg struct {
	artifact *artifactv1.Artifact
	related  []*artifactv1.Artifact
}

type artifactEventMsg struct {
	event *agentv1.ArtifactEvent
}

type errorMsg struct{ err error }

type statusMsg struct{ text string }

// Model is the root Bubbletea model for the Clara TUI.
type Model struct {
	client  *tuigrpc.Client
	ctx     context.Context
	cancel  context.CancelFunc
	logger  zerolog.Logger

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
func New(client *tuigrpc.Client, logger zerolog.Logger) Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := Model{
		client:    client,
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger,
		artifacts: panes.NewArtifactsPane(),
		related:   panes.NewRelatedPane(),
		detail:    panes.NewDetailPane(),
		focus:     paneArtifacts,
	}
	m.artifacts.SetFocused(true)
	return m
}

// Init fires the initial commands.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadArtifacts(),
		m.subscribeToAgent(),
	)
}

// Update handles all messages.
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
		// Load detail for first item.
		if sel := m.artifacts.Selected(); sel != nil {
			return m, m.loadDetail(sel.Id)
		}

	case artifactDetailMsg:
		m.detail.SetArtifact(msg.artifact)
		m.related.SetRelated(msg.related)

	case artifactEventMsg:
		// Refresh list on any event.
		return m, m.loadArtifacts()

	case statusMsg:
		m.status = msg.text

	case errorMsg:
		m.err = msg.err.Error()
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys.
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
	case "l":
		if m.focus < paneDetail {
			m.cycleFocus(1)
			return m, nil
		}
	case "h":
		if m.focus > paneArtifacts {
			m.cycleFocus(-1)
			return m, nil
		}
	}

	// Delegate to focused pane.
	switch m.focus {
	case paneArtifacts:
		action := m.artifacts.Update(msg)
		if action != "" {
			return m, m.handleAction(action)
		}
		// After navigation, load detail for new selection.
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

// handleAction executes a pane action (done, edit, open).
func (m *Model) handleAction(action string) tea.Cmd {
	if len(action) < 6 {
		return nil
	}
	verb := action[:len(action)-len(action[5:])-1]
	id := action[len(verb)+1:]

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
	if sel == nil || sel.SourcePath == "" {
		return nil
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	cmd := exec.Command(editor, sel.SourcePath) //nolint:gosec
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return errorMsg{err}
		}
		return statusMsg{"editor closed"}
	})
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
	return false // TODO: expose from artifacts pane if needed
}

func (m *Model) cycleFocus(delta int) {
	m.setFocus(focusedPane((int(m.focus) + delta + PaneCount) % PaneCount))
}

func (m *Model) setFocus(f focusedPane) {
	m.focus = f
	m.artifacts.SetFocused(f == paneArtifacts)
	m.related.SetFocused(f == paneRelated)
	m.detail.SetFocused(f == paneDetail)
}

func (m *Model) updatePaneSizes() {
	if m.width == 0 || m.height == 0 {
		return
	}
	sidebarW := m.width * 35 / 100
	detailW := m.width - sidebarW

	// Accordion: focused pane gets 65% of sidebar height, other 35%.
	var artifactsH, relatedH int
	switch m.focus {
	case paneArtifacts:
		artifactsH = m.height * 65 / 100
		relatedH = m.height - artifactsH - 1
	case paneRelated:
		relatedH = m.height * 65 / 100
		artifactsH = m.height - relatedH - 1
	default:
		artifactsH = m.height / 2
		relatedH = m.height - artifactsH - 1
	}
	// Minimum height for collapsed panes.
	if artifactsH < 3 {
		artifactsH = 3
	}
	if relatedH < 3 {
		relatedH = 3
	}

	m.artifacts.SetSize(sidebarW, artifactsH)
	m.related.SetSize(sidebarW, relatedH)
	m.detail.SetSize(detailW, m.height-1)
}

// View renders the full TUI layout.
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

	statusBar := m.renderStatusBar()
	return lipgloss.JoinVertical(lipgloss.Left, main, statusBar)
}

func (m Model) renderStatusBar() string {
	help := styles.Muted.Render(" j/k:nav  Tab:focus  /:search  Space:done  Enter:edit  o:open  q:quit")
	statusText := m.status
	if m.err != "" {
		statusText = styles.Muted.Render("ERR: " + m.err)
	}
	bar := lipgloss.NewStyle().
		Width(m.width).
		Background(lipgloss.Color("#181825")).
		Foreground(styles.ColorMuted).
		Render(fmt.Sprintf(" clara  %s  %s", statusText, help))
	return bar
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
			return errorMsg{err}
		}
		// Return a command that reads from the channel.
		return waitForEvent(ch)
	}
}

// waitForEvent is a blocking command that reads one event from the subscription channel.
func waitForEvent(ch <-chan *agentv1.ArtifactEvent) tea.Msg {
	ev, ok := <-ch
	if !ok {
		return nil
	}
	return artifactEventMsg{event: ev}
}
