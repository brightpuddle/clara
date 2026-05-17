package webui

import (
	"bufio"
	"net/http"
	"os"

	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

// handleDashboard renders the overview/dashboard page.
func (w *WebUI) handleDashboard(c echo.Context) error {
	infos := w.sup.IntentInfos()
	active := 0
	for _, i := range infos {
		if i.Active {
			active++
		}
	}

	integrations := w.integ.List()

	// Read last 30 lines of daemon log
	logLines := tailFile(w.cfg.LogPath(), 30)

	return render(c, http.StatusOK, ui.Dashboard(ui.DashboardData{
		IntentCount:       len(infos),
		ActiveIntentCount: active,
		ToolCount:         len(w.reg.Names()),
		Integrations:      integrations,
		RecentLogs:        logLines,
	}))
}

// tailFile reads the last n lines from a file, returning an empty slice on
// error (file may not exist yet).
func tailFile(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Read all lines into a ring buffer of size n.
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
