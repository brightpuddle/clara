package webui

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/brightpuddle/clara/internal/supervisor"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

// handleActuatorsList renders the list of all available actuators.
func (w *WebUI) handleActuatorsList(c echo.Context) error {
	ctx := c.Request().Context()
	var automations []supervisor.AutomationSummary
	if w.evaluator != nil {
		var err error
		automations, err = w.evaluator.AutomationsOverview(ctx)
		if err != nil {
			w.log.Warn().Err(err).Msg("failed to list automations")
		}
	}

	vm := &ui.ActuatorsVM{
		Base:        w.baseVM("Actuators", "/ui/actuators"),
		Automations: automations,
	}

	return render(c, http.StatusOK, ui.ActuatorList(vm))
}

// handleActuatorDetail renders the detail view for a single actuator.
func (w *WebUI) handleActuatorDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	var found *supervisor.AutomationSummary
	if w.evaluator != nil {
		automations, err := w.evaluator.AutomationsOverview(ctx)
		if err == nil {
			for _, auto := range automations {
				if auto.ActuatorID == id {
					autoCopy := auto
					found = &autoCopy
					break
				}
			}
		}
	}

	if found == nil {
		found = &supervisor.AutomationSummary{
			ActuatorID:  id,
			Name:        id,
			Description: "Actuator",
			Routing:     "llm-dynamic",
		}
	}

	logPath := filepath.Join(w.cfg.DataDir, "logs", id+".jsonl")
	logLines := tailFile(logPath, 50)

	vm := &ui.ActuatorDetailVM{
		Base:       w.baseVM(fmt.Sprintf("Actuator: %s", found.Name), "/ui/actuators"),
		Summary:    *found,
		RecentLogs: logLines,
	}

	return render(c, http.StatusOK, ui.ActuatorDetail(vm))
}

// handleActuatorRun triggers a manual actuator run.
func (w *WebUI) handleActuatorRun(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	if err := w.sup.RunActuator(ctx, id, nil); err != nil {
		return render(c, http.StatusOK, ui.ActuatorRunResult(false, fmt.Sprintf("Error running actuator: %v", err)))
	}

	return render(c, http.StatusOK, ui.ActuatorRunResult(true, fmt.Sprintf("Actuator %s dispatched successfully", id)))
}
