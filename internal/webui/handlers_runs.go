package webui

import (
	"net/http"

	"github.com/brightpuddle/clara/internal/store"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

func (w *WebUI) handleRunsList(c echo.Context) error {
	ctx := c.Request().Context()
	selectedID := c.QueryParam("id")
	triggerFilter := c.QueryParam("trigger")

	var runs []store.TriggerRunRecord
	var selectedRun *store.TriggerRunRecord
	var toolCalls []store.ToolCallRecord

	if w.db != nil {
		var err error
		runs, err = w.db.ListTriggerRuns(ctx, 50, triggerFilter)
		if err != nil {
			w.log.Warn().Err(err).Msg("failed to load runs for runs page")
		}

		if selectedID != "" {
			selectedRun, _ = w.db.GetTriggerRun(ctx, selectedID)
			if selectedRun != nil {
				toolCalls, _ = w.db.ListToolCalls(ctx, 50, selectedID)
			}
		}
	}

	vm := &ui.RunsVM{
		Base:          w.baseVM("Execution Runs", "/ui/runs"),
		Runs:          runs,
		SelectedRun:   selectedRun,
		ToolCalls:     toolCalls,
		TriggerFilter: triggerFilter,
	}

	return render(c, http.StatusOK, ui.Runs(vm))
}
