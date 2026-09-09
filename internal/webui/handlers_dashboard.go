package webui

import (
	"bufio"
	"net/http"
	"os"

	"github.com/brightpuddle/clara/internal/supervisor"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

// handleDashboard renders the overview/dashboard page.
func (w *WebUI) handleDashboard(c echo.Context) error {
	ctx := c.Request().Context()
	var automations []supervisor.AutomationSummary
	if w.evaluator != nil {
		var err error
		automations, err = w.evaluator.AutomationsOverview(ctx)
		if err != nil {
			w.log.Warn().Err(err).Msg("failed to load automations overview for dashboard")
		}
	}

	integrations := w.integ.List()
	toolsCount := len(w.reg.Names())
	pendingApprovals := 0
	if w.approvals != nil {
		pendingApprovals = len(w.approvals.List())
	}

	// Read last 30 lines of daemon log
	logLines := tailFile(w.cfg.LogPath(), 30)

	vm := &ui.DashboardVM{
		Base:              w.baseVM("Dashboard", "/ui/"),
		ActuatorsCount:    len(automations),
		ToolsCount:        toolsCount,
		IntegrationsCount: len(integrations),
		PendingApprovals:  pendingApprovals,
		Automations:       automations,
		Integrations:      integrations,
		RecentLogs:        logLines,
	}

	return render(c, http.StatusOK, ui.Dashboard(vm))
}

// tailFile reads the last n lines from a file, returning an empty slice on error.
func tailFile(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 256*1024), 256*1024)
	for sc.Scan() {
		if len(ring) >= n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return ring
}
