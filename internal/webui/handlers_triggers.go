package webui

import (
	"encoding/json"
	"net/http"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

func (w *WebUI) handleTriggersList(c echo.Context) error {
	ctx := c.Request().Context()
	var triggers []trigger.Definition
	if w.triggerMgr != nil {
		triggers = w.triggerMgr.List()
	}

	var recentRuns []store.TriggerRunRecord
	if w.db != nil {
		var err error
		recentRuns, err = w.db.ListTriggerRuns(ctx, 20, "")
		if err != nil {
			w.log.Warn().Err(err).Msg("failed to load recent runs for triggers page")
		}
	}

	vm := &ui.TriggersVM{
		Base:       w.baseVM("Triggers", "/ui/triggers"),
		Triggers:   triggers,
		RecentRuns: recentRuns,
	}

	return render(c, http.StatusOK, ui.Triggers(vm))
}

func (w *WebUI) handleTriggerDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	if w.triggerMgr == nil {
		return c.String(http.StatusServiceUnavailable, "Trigger manager not available")
	}

	def, ok := w.triggerMgr.Get(id)
	if !ok {
		return c.String(http.StatusNotFound, "Trigger not found")
	}

	var recentRuns []store.TriggerRunRecord
	if w.db != nil {
		recentRuns, _ = w.db.ListTriggerRuns(ctx, 20, id)
	}

	vm := &ui.TriggerDetailVM{
		Base:       w.baseVM("Trigger: "+id, "/ui/triggers"),
		Trigger:    *def,
		RecentRuns: recentRuns,
	}

	return render(c, http.StatusOK, ui.TriggerDetail(vm))
}

func (w *WebUI) handleTriggerRun(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	if w.triggerMgr == nil {
		return c.String(http.StatusServiceUnavailable, "Trigger manager not available")
	}

	def, ok := w.triggerMgr.Get(id)
	if !ok {
		return c.String(http.StatusNotFound, "Trigger not found")
	}

	eventJSONStr := c.FormValue("event_json")
	var eventData any
	if eventJSONStr != "" {
		_ = json.Unmarshal([]byte(eventJSONStr), &eventData)
	}

	record, err := w.triggerMgr.Run(ctx, id, eventData)
	if err != nil {
		w.log.Error().Err(err).Str("trigger", id).Msg("failed to execute trigger action")
	}

	var recentRuns []store.TriggerRunRecord
	if w.db != nil {
		recentRuns, _ = w.db.ListTriggerRuns(ctx, 20, id)
	}

	vm := &ui.TriggerDetailVM{
		Base:       w.baseVM("Trigger: "+id, "/ui/triggers"),
		Trigger:    *def,
		RecentRuns: recentRuns,
		RunResult:  record,
	}

	return render(c, http.StatusOK, ui.TriggerDetail(vm))
}
