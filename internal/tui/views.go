package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ViewRenderer provides panel and layout rendering.
type ViewRenderer struct {
	state  *AppState
	width  int
	height int
}

// NewViewRenderer creates a renderer for the current state.
func NewViewRenderer(state *AppState, width, height int) *ViewRenderer {
	return &ViewRenderer{
		state:  state,
		width:  width,
		height: height,
	}
}

// RenderEventsPanel renders Panel 1: Ingress CloudEvents and Trace Status.
func (r *ViewRenderer) RenderEventsPanel(active bool, w, h int) string {
	title := PanelTitleStyle(active).Render(" 1: Events ")
	events := r.state.FilteredEvents()

	if len(events) == 0 {
		emptyMsg := StyleDim.Render("No events ingested yet")
		if r.state.FilterText != "" {
			emptyMsg = StyleDim.Render("No events match filter")
		}
		return PanelStyle(active, w, h).Render(title + "\n\n  " + emptyMsg)
	}

	contentLines := make([]string, 0, h)
	contentLines = append(contentLines, title)

	// Visible height inside panel (minus borders and title)
	innerH := h - 3
	if innerH < 1 {
		innerH = 1
	}

	start := 0
	if r.state.EventIdx >= innerH {
		start = r.state.EventIdx - innerH + 1
	}
	end := start + innerH
	if end > len(events) {
		end = len(events)
	}

	for i := start; i < end; i++ {
		ev := events[i]
		icon := StyleSuccess.Render("●")
		switch ev.Status {
		case "error":
			icon = StyleError.Render("✕")
		case "blocked":
			icon = StyleWarning.Render("!")
		case "skipped":
			icon = StyleDim.Render("◌")
		case "pending":
			icon = StyleCyan.Render("…")
		}

		timeStr := ev.Time.Format("15:04:05")
		typeStr := ev.Type
		maxTypeLen := w - 22
		if maxTypeLen > 5 && len(typeStr) > maxTypeLen {
			typeStr = typeStr[:maxTypeLen-3] + "..."
		}

		line := fmt.Sprintf("%s %-8s %s", icon, timeStr, typeStr)

		if i == r.state.EventIdx && active {
			// Pad to width
			padded := line + strings.Repeat(" ", max(0, w-lipgloss.Width(line)-4))
			contentLines = append(contentLines, SelectedItemStyle.Render(padded))
		} else {
			contentLines = append(contentLines, NormalItemStyle.Render(line))
		}
	}

	return PanelStyle(active, w, h).Render(strings.Join(contentLines, "\n"))
}

// RenderRulesPanel renders Panel 2: Evaluator Heuristics & Automations.
func (r *ViewRenderer) RenderRulesPanel(active bool, w, h int) string {
	title := PanelTitleStyle(active).Render(" 2: Evaluator / Rules ")
	rules := r.state.FilteredRules()

	if len(rules) == 0 {
		emptyMsg := StyleDim.Render("No active rules")
		return PanelStyle(active, w, h).Render(title + "\n\n  " + emptyMsg)
	}

	contentLines := make([]string, 0, h)
	contentLines = append(contentLines, title)

	innerH := h - 3
	if innerH < 1 {
		innerH = 1
	}

	start := 0
	if r.state.RuleIdx >= innerH {
		start = r.state.RuleIdx - innerH + 1
	}
	end := start + innerH
	if end > len(rules) {
		end = len(rules)
	}

	for i := start; i < end; i++ {
		rule := rules[i]
		badge := StyleCyan.Render("[H]")
		if rule.Routing == "llm-dynamic" {
			badge = StylePurple.Render("[L]")
		}

		name := rule.Name
		if name == "" {
			name = rule.ActuatorID
		}
		maxLen := w - 12
		if maxLen > 5 && len(name) > maxLen {
			name = name[:maxLen-3] + "..."
		}

		line := fmt.Sprintf("%s %s", badge, name)
		if i == r.state.RuleIdx && active {
			padded := line + strings.Repeat(" ", max(0, w-lipgloss.Width(line)-4))
			contentLines = append(contentLines, SelectedItemStyle.Render(padded))
		} else {
			contentLines = append(contentLines, NormalItemStyle.Render(line))
		}
	}

	return PanelStyle(active, w, h).Render(strings.Join(contentLines, "\n"))
}

// RenderActuatorsPanel renders Panel 3: Loaded Actuators & Health.
func (r *ViewRenderer) RenderActuatorsPanel(active bool, w, h int) string {
	title := PanelTitleStyle(active).Render(" 3: Actuators / Tools ")
	actuators := r.state.FilteredActuators()

	if len(actuators) == 0 {
		emptyMsg := StyleDim.Render("No actuators loaded")
		return PanelStyle(active, w, h).Render(title + "\n\n  " + emptyMsg)
	}

	contentLines := make([]string, 0, h)
	contentLines = append(contentLines, title)

	innerH := h - 3
	if innerH < 1 {
		innerH = 1
	}

	start := 0
	if r.state.ActuatorIdx >= innerH {
		start = r.state.ActuatorIdx - innerH + 1
	}
	end := start + innerH
	if end > len(actuators) {
		end = len(actuators)
	}

	for i := start; i < end; i++ {
		a := actuators[i]
		icon := StyleSuccess.Render("●")
		if a.Status == "error" || a.FailCount > 0 {
			icon = StyleError.Render("✕")
		} else if a.Status == "idle" {
			icon = StyleDim.Render("○")
		}

		name := a.ID
		maxLen := w - 14
		if maxLen > 5 && len(name) > maxLen {
			name = name[:maxLen-3] + "..."
		}

		stat := ""
		if a.ExecCount > 0 {
			stat = fmt.Sprintf("(%d)", a.ExecCount)
		}

		line := fmt.Sprintf("%s %s %s", icon, name, StyleDim.Render(stat))
		if i == r.state.ActuatorIdx && active {
			padded := line + strings.Repeat(" ", max(0, w-lipgloss.Width(line)-4))
			contentLines = append(contentLines, SelectedItemStyle.Render(padded))
		} else {
			contentLines = append(contentLines, NormalItemStyle.Render(line))
		}
	}

	return PanelStyle(active, w, h).Render(strings.Join(contentLines, "\n"))
}

// RenderApprovalsPanel renders Panel 4: HITL Approvals Queue.
func (r *ViewRenderer) RenderApprovalsPanel(active bool, w, h int) string {
	title := PanelTitleStyle(active).Render(" 4: Approvals (HITL) ")
	approvals := r.state.Approvals

	if len(approvals) == 0 {
		emptyMsg := StyleDim.Render("No pending approvals")
		return PanelStyle(active, w, h).Render(title + "\n\n  " + emptyMsg)
	}

	contentLines := make([]string, 0, h)
	contentLines = append(contentLines, title)

	innerH := h - 3
	if innerH < 1 {
		innerH = 1
	}

	start := 0
	if r.state.ApprovalIdx >= innerH {
		start = r.state.ApprovalIdx - innerH + 1
	}
	end := start + innerH
	if end > len(approvals) {
		end = len(approvals)
	}

	for i := start; i < end; i++ {
		app := approvals[i]
		icon := StyleWarning.Render("!")
		text := app.RequestID
		if len(text) > w-8 {
			text = text[:w-11] + "..."
		}

		line := fmt.Sprintf("%s %s", icon, text)
		if i == r.state.ApprovalIdx && active {
			padded := line + strings.Repeat(" ", max(0, w-lipgloss.Width(line)-4))
			contentLines = append(contentLines, SelectedItemStyle.Render(padded))
		} else {
			contentLines = append(contentLines, NormalItemStyle.Render(line))
		}
	}

	return PanelStyle(active, w, h).Render(strings.Join(contentLines, "\n"))
}

// RenderInspectorPanel renders Panel 5: Detail Inspector for the focused item.
func (r *ViewRenderer) RenderInspectorPanel(activePanel Panel, w, h int) string {
	title := PanelTitleStyle(activePanel == PanelInspector).Render(" 5: Main Inspector ")
	lines := make([]string, 0, h)
	lines = append(lines, title, "")

	switch activePanel {
	case PanelEvents, PanelInspector:
		events := r.state.FilteredEvents()
		if len(events) == 0 || r.state.EventIdx >= len(events) {
			lines = append(lines, StyleDim.Render("No event selected"))
			break
		}
		ev := events[r.state.EventIdx]

		statusBadge := StyleSuccess.Render("[SUCCESS]")
		switch ev.Status {
		case "error":
			statusBadge = StyleError.Render("[ERROR]")
		case "blocked":
			statusBadge = StyleWarning.Render("[BLOCKED / HITL]")
		case "skipped":
			statusBadge = StyleDim.Render("[SKIPPED]")
		case "pending":
			statusBadge = StyleCyan.Render("[PENDING]")
		}

		lines = append(lines,
			fmt.Sprintf(" %s %s  %s", StyleDim.Render("Event:"), StyleCyan.Render(ev.ID), statusBadge),
			fmt.Sprintf(" %s %s", StyleDim.Render("Type: "), ev.Type),
			fmt.Sprintf(" %s %s", StyleDim.Render("Source:"), ev.Source),
			fmt.Sprintf(" %s %s", StyleDim.Render("Time:  "), ev.Time.Format(time.RFC3339)),
		)

		if ev.ActuatorID != "" {
			lines = append(lines, fmt.Sprintf(" %s %s (%s)", StyleDim.Render("Action:"), StyleCyan.Render(ev.ActuatorID), ev.Routing))
		}

		lines = append(lines, "", StyleDim.Render(" ── Payload Data ───────────────────────────────────────"))
		if len(ev.Data) > 0 {
			jsonStr := PrettyJSON(ev.Data)
			for _, jl := range strings.Split(jsonStr, "\n") {
				lines = append(lines, "  "+jl)
			}
		} else {
			lines = append(lines, "  "+StyleDim.Render("{}"))
		}

		if len(ev.Logs) > 0 {
			lines = append(lines, "", StyleDim.Render(" ── Trace & Logs ───────────────────────────────────────"))
			for _, l := range ev.Logs {
				lines = append(lines, "  "+l)
			}
		}

	case PanelRules:
		rules := r.state.FilteredRules()
		if len(rules) == 0 || r.state.RuleIdx >= len(rules) {
			lines = append(lines, StyleDim.Render("No rule selected"))
			break
		}
		rItem := rules[r.state.RuleIdx]

		lines = append(lines,
			fmt.Sprintf(" %s %s", StyleDim.Render("Rule / Automation:"), StyleCyan.Render(rItem.Name)),
			fmt.Sprintf(" %s %s", StyleDim.Render("Target Actuator:  "), StyleCyan.Render(rItem.ActuatorID)),
			fmt.Sprintf(" %s %s", StyleDim.Render("Routing Mode:     "), rItem.Routing),
		)

		if rItem.TTL != "" {
			lines = append(lines, fmt.Sprintf(" %s %s (expires: %s)", StyleDim.Render("Cache TTL:        "), rItem.TTL, rItem.ExpiresIn))
		}
		if rItem.Description != "" {
			lines = append(lines, fmt.Sprintf(" %s %s", StyleDim.Render("Description:      "), rItem.Description))
		}
		if len(rItem.Triggers) > 0 {
			lines = append(lines, fmt.Sprintf(" %s %s", StyleDim.Render("Triggers:         "), strings.Join(rItem.Triggers, ", ")))
		}

		if len(rItem.Capabilities) > 0 {
			lines = append(lines, "", StyleDim.Render(" ── Declared Capabilities (CBAC) ──────────────────────"))
			for _, c := range rItem.Capabilities {
				lines = append(lines, fmt.Sprintf("  • %s (%s)", StyleCyan.Render(c.Resource), c.Description))
			}
		}

		lines = append(lines, "", StyleDim.Render(" Press [e] to edit rule configuration in $EDITOR"))

	case PanelActuators:
		actuators := r.state.FilteredActuators()
		if len(actuators) == 0 || r.state.ActuatorIdx >= len(actuators) {
			lines = append(lines, StyleDim.Render("No actuator selected"))
			break
		}
		a := actuators[r.state.ActuatorIdx]

		healthBadge := StyleSuccess.Render("[HEALTHY]")
		if a.Status == "error" || a.FailCount > 0 {
			healthBadge = StyleError.Render(fmt.Sprintf("[FAILING: %d fails]", a.FailCount))
		} else if a.Status == "idle" {
			healthBadge = StyleDim.Render("[IDLE]")
		}

		lines = append(lines,
			fmt.Sprintf(" %s %s  %s", StyleDim.Render("Actuator:"), StyleCyan.Render(a.ID), healthBadge),
			fmt.Sprintf(" %s %d", StyleDim.Render("Executions:"), a.ExecCount),
			fmt.Sprintf(" %s %d", StyleDim.Render("Failures:  "), a.FailCount),
		)

		if !a.LastRun.IsZero() {
			lines = append(lines, fmt.Sprintf(" %s %s", StyleDim.Render("Last Run:  "), a.LastRun.Format(time.RFC3339)))
		}
		if a.Description != "" {
			lines = append(lines, fmt.Sprintf(" %s %s", StyleDim.Render("Description:"), a.Description))
		}

		if len(a.Capabilities) > 0 {
			lines = append(lines, "", StyleDim.Render(" ── Capabilities ───────────────────────────────────────"))
			for _, c := range a.Capabilities {
				lines = append(lines, fmt.Sprintf("  • %s (%s)", StyleCyan.Render(c.Resource), c.Description))
			}
		}

		if a.LastError != "" {
			lines = append(lines, "", StyleError.Render(" ── Last Error ─────────────────────────────────────────"))
			for _, el := range strings.Split(a.LastError, "\n") {
				lines = append(lines, "  "+StyleError.Render(el))
			}
		}

		if a.LastOutput != "" {
			lines = append(lines, "", StyleDim.Render(" ── Last Output ────────────────────────────────────────"))
			for _, ol := range strings.Split(a.LastOutput, "\n") {
				lines = append(lines, "  "+ol)
			}
		}

	case PanelApprovals:
		approvals := r.state.Approvals
		if len(approvals) == 0 || r.state.ApprovalIdx >= len(approvals) {
			lines = append(lines, StyleDim.Render("No pending approval selected"))
			break
		}
		app := approvals[r.state.ApprovalIdx]

		lines = append(lines,
			fmt.Sprintf(" %s %s", StyleWarning.Render("Pending Approval Request:"), StyleCyan.Render(app.RequestID)),
			"",
			StyleDim.Render(" ── Context / Reason ───────────────────────────────────"),
			"  "+app.Context,
			"",
			StyleDim.Render(" ── Resolution Options ─────────────────────────────────"),
		)

		for idx, opt := range app.Options {
			lines = append(lines, fmt.Sprintf("  [%d] %s (%s)", idx+1, opt.Description, StyleCyan.Render(opt.ActionCode)))
		}

		lines = append(lines, "",
			StyleDim.Render(" Press [space] or [1-9] to Approve, [d] to Deny"),
		)
	}

	// Truncate to height if necessary
	if len(lines) > h-2 {
		lines = lines[:h-2]
	}

	return PanelStyle(activePanel == PanelInspector, w, h).Render(strings.Join(lines, "\n"))
}

// RenderStatusBar renders the bottom keybindings status line.
func (r *ViewRenderer) RenderStatusBar(online bool, statusMsg string) string {
	keys := []struct {
		key  string
		desc string
	}{
		{"1-4", "panels"},
		{"j/k", "navigate"},
		{"space", "approve"},
		{"d", "deny"},
		{"e", "edit"},
		{"/", "filter"},
		{"r", "refresh"},
		{"?", "help"},
		{"q", "quit"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts, StatusBarKeyStyle.Render(k.key)+StatusBarDescStyle.Render(k.desc))
	}
	keybar := strings.Join(parts, " ")

	badge := StatusBarOnlineBadge.Render(" ONLINE ")
	if !online {
		badge = StatusBarOfflineBadge.Render(" OFFLINE ")
	}

	availW := r.width - lipgloss.Width(keybar) - lipgloss.Width(badge) - 2
	if availW < 0 {
		availW = 0
	}
	padding := strings.Repeat(" ", availW)

	return lipgloss.JoinHorizontal(lipgloss.Center, keybar, padding, badge)
}

// RenderHelpModal renders the popup help dialog.
func (r *ViewRenderer) RenderHelpModal() string {
	helpContent := `
 Clara TUI - Keyboard Navigation (lazygit style)

 Navigation:
   1, 2, 3, 4      Jump directly to panel
   tab, [ / ]      Cycle active panel
   j / k (↓ / ↑)   Navigate item list
   g / G           Jump to top / bottom

 Actions:
   space / enter   Approve HITL request / inspect details
   d               Deny approval / dismiss
   e               Open rule or config in $EDITOR
   /               Filter / search in current panel
   esc             Clear search filter / close modal
   r               Manual refresh from daemon
   ?               Toggle this help screen
   q / ctrl+c      Quit TUI
`
	return ModalStyle.Render(helpContent)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
