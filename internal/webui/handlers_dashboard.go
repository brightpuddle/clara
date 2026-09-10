package webui

import (
	"bufio"
	"net/http"
	"os"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

// handleDashboard renders the overview/dashboard page.
func (w *WebUI) handleDashboard(c echo.Context) error {
	ctx := c.Request().Context()

	var triggers []trigger.Definition
	if w.triggerMgr != nil {
		triggers = w.triggerMgr.List()
	}

	eventCount := 0
	schedCount := 0
	workerCount := 0
	for _, t := range triggers {
		switch t.Type {
		case trigger.TypeEvent:
			eventCount++
		case trigger.TypeSchedule:
			schedCount++
		case trigger.TypeWorker:
			workerCount++
		}
	}

	var recentRuns []store.TriggerRunRecord
	if w.db != nil {
		recentRuns, _ = w.db.ListTriggerRuns(ctx, 10, "")
	}

	var integrations []map[string]any
	if w.integ != nil {
		integrations = w.integ.List()
	}
	toolsCount := 0
	if w.reg != nil {
		toolsCount = len(w.reg.Names())
	}

	// Read last 30 lines of daemon log
	logLines := tailFile(w.cfg.LogPath(), 30)

	vm := &ui.DashboardVM{
		Base:                  w.baseVM("Dashboard", "/ui/"),
		TriggersCount:         len(triggers),
		EventTriggersCount:    eventCount,
		ScheduleTriggersCount: schedCount,
		WorkerTriggersCount:   workerCount,
		ToolsCount:            toolsCount,
		IntegrationsCount:     len(integrations),
		Triggers:              triggers,
		RecentRuns:            recentRuns,
		Integrations:          integrations,
		RecentLogs:            logLines,
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
