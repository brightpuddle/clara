package webui

import (
	"fmt"
	"net/http"

	"github.com/brightpuddle/clara/internal/supervisor"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

// handleIntentList renders the list of all registered intents.
func (w *WebUI) handleIntentList(c echo.Context) error {
	infos := w.sup.IntentInfos()
	return render(c, http.StatusOK, ui.IntentList(ui.IntentListData{
		Intents: infos,
	}))
}

// handleIntentDetail renders the detail view for a single intent.
func (w *WebUI) handleIntentDetail(c echo.Context) error {
	id := c.Param("id")
	infos := w.sup.IntentInfos()
	var found *supervisor.IntentInfo
	for _, info := range infos {
		info := info
		if info.ID == id {
			found = &info
			break
		}
	}
	if found == nil {
		return echo.NewHTTPError(http.StatusNotFound, "intent not found")
	}

	logLines := tailFile(w.ilog.FilePath(id), 50)

	return render(c, http.StatusOK, ui.IntentDetail(ui.IntentDetailData{
		Info:     *found,
		LogLines: logLines,
	}))
}

// handleIntentRun triggers an on-demand intent and returns an HTMX partial.
func (w *WebUI) handleIntentRun(c echo.Context) error {
	id := c.Param("id")
	err := w.sup.StartIntent(id, "")
	if err != nil {
		return render(c, http.StatusOK, ui.IntentRunResult(false, fmt.Sprintf("Error: %s", err)))
	}
	return render(c, http.StatusOK, ui.IntentRunResult(true, "Intent "+id+" started"))
}
