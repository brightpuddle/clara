package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

// handleAPIDashboard returns dashboard summary data as JSON.
func (w *WebUI) handleAPIDashboard(c echo.Context) error {
	ctx := c.Request().Context()

	triggers := make([]trigger.Definition, 0)
	if w.triggerMgr != nil {
		triggers = w.triggerMgr.List()
	}

	eventCount := 0
	schedCount := 0
	workerCount := 0
	manualCount := 0
	for _, t := range triggers {
		switch t.Type {
		case trigger.TypeEvent:
			eventCount++
		case trigger.TypeSchedule:
			schedCount++
		case trigger.TypeWorker:
			workerCount++
		case trigger.TypeManual:
			manualCount++
		}
	}

	recentRuns := make([]store.TriggerRunRecord, 0)
	if w.db != nil {
		if runs, err := w.db.ListTriggerRuns(ctx, 10, ""); err == nil && runs != nil {
			recentRuns = runs
		}
	}

	integrations := make([]map[string]any, 0)
	if w.integ != nil {
		if list := w.integ.List(); list != nil {
			integrations = list
		}
	}
	toolsCount := 0
	if w.reg != nil {
		toolsCount = len(w.reg.Names())
	}

	logLines := tailFile(w.cfg.LogPath(), 30)

	return c.JSON(http.StatusOK, map[string]any{
		"triggers_count":          len(triggers),
		"event_triggers_count":    eventCount,
		"schedule_triggers_count": schedCount,
		"worker_triggers_count":   workerCount,
		"manual_triggers_count":   manualCount,
		"tools_count":             toolsCount,
		"integrations_count":      len(integrations),
		"triggers":                triggers,
		"recent_runs":             recentRuns,
		"integrations":            integrations,
		"recent_logs":             logLines,
	})
}

// handleAPITriggersList returns the list of triggers and recent runs.
func (w *WebUI) handleAPITriggersList(c echo.Context) error {
	ctx := c.Request().Context()

	triggers := make([]trigger.Definition, 0)
	if w.triggerMgr != nil {
		triggers = w.triggerMgr.List()
	}

	sort.Slice(triggers, func(i, j int) bool {
		return triggers[i].ID < triggers[j].ID
	})

	recentRuns := make([]store.TriggerRunRecord, 0)
	if w.db != nil {
		if runs, err := w.db.ListTriggerRuns(ctx, 20, ""); err == nil && runs != nil {
			recentRuns = runs
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"triggers":    triggers,
		"recent_runs": recentRuns,
	})
}

// handleAPITriggerDetail returns details for a single trigger.
func (w *WebUI) handleAPITriggerDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	if w.triggerMgr == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "Trigger manager not available"})
	}

	def, ok := w.triggerMgr.Get(id)
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Trigger not found"})
	}

	recentRuns := make([]store.TriggerRunRecord, 0)
	if w.db != nil {
		if runs, err := w.db.ListTriggerRuns(ctx, 20, id); err == nil && runs != nil {
			recentRuns = runs
		}
	}

	filePath := w.triggerMgr.GetFilePath(id)

	return c.JSON(http.StatusOK, map[string]any{
		"trigger":     def,
		"recent_runs": recentRuns,
		"file_path":   filePath,
	})
}

type TriggerCreateUpdateReq struct {
	TriggerJSON string `json:"trigger_json"`
	YAML        string `json:"yaml"`
}

// handleAPITriggerCreate creates a new trigger definition.
func (w *WebUI) handleAPITriggerCreate(c echo.Context) error {
	var body TriggerCreateUpdateReq
	if err := c.Bind(&body); err != nil {
		body.TriggerJSON = c.FormValue("trigger_json")
		body.YAML = c.FormValue("yaml")
	}
	if body.TriggerJSON == "" {
		body.TriggerJSON = c.FormValue("trigger_json")
	}
	if body.YAML == "" {
		body.YAML = c.FormValue("yaml")
	}

	var def *trigger.Definition
	var err error

	if body.TriggerJSON != "" {
		var st StructuredTrigger
		if unmarshalErr := json.Unmarshal([]byte(body.TriggerJSON), &st); unmarshalErr != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid trigger json: " + unmarshalErr.Error()})
		}
		def, err = structuredToTrigger(&st)
	} else if body.YAML != "" {
		def, err = yamlToTrigger(body.YAML)
	} else {
		var st StructuredTrigger
		if bindErr := c.Bind(&st); bindErr == nil && st.ID != "" {
			def, err = structuredToTrigger(&st)
		} else {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "no trigger definition provided"})
		}
	}

	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if def.ID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "trigger ID is required"})
	}

	if w.triggerMgr == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "Trigger manager not available"})
	}

	if _, exists := w.triggerMgr.Get(def.ID); exists {
		return c.JSON(http.StatusConflict, map[string]string{"error": "trigger with this ID already exists"})
	}

	if _, err := w.triggerMgr.SaveToFile(*def, w.primaryTriggerDir()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save trigger: " + err.Error()})
	}

	return c.Redirect(http.StatusSeeOther, "/ui/triggers/"+url.PathEscape(def.ID)+"?flash=Trigger+created+successfully&flash_kind=success")
}

// handleAPITriggerUpdate updates an existing trigger.
func (w *WebUI) handleAPITriggerUpdate(c echo.Context) error {
	id := c.Param("id")
	if w.triggerMgr == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "Trigger manager not available"})
	}

	if _, ok := w.triggerMgr.Get(id); !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Trigger not found"})
	}

	var body TriggerCreateUpdateReq
	if err := c.Bind(&body); err != nil {
		body.TriggerJSON = c.FormValue("trigger_json")
		body.YAML = c.FormValue("yaml")
	}
	if body.TriggerJSON == "" {
		body.TriggerJSON = c.FormValue("trigger_json")
	}
	if body.YAML == "" {
		body.YAML = c.FormValue("yaml")
	}

	var def *trigger.Definition
	var err error

	if body.TriggerJSON != "" {
		var st StructuredTrigger
		if unmarshalErr := json.Unmarshal([]byte(body.TriggerJSON), &st); unmarshalErr != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid trigger json: " + unmarshalErr.Error()})
		}
		def, err = structuredToTrigger(&st)
	} else if body.YAML != "" {
		def, err = yamlToTrigger(body.YAML)
	} else {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no trigger definition provided"})
	}

	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	if def.ID == "" {
		def.ID = id
	}

	if def.ID != id {
		if _, exists := w.triggerMgr.Get(def.ID); exists {
			return c.JSON(http.StatusConflict, map[string]string{"error": fmt.Sprintf("Trigger ID %q already exists", def.ID)})
		}
		_ = w.triggerMgr.DeleteTrigger(id)
	}

	if _, err := w.triggerMgr.SaveToFile(*def, w.primaryTriggerDir()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save trigger: " + err.Error()})
	}

	return c.Redirect(http.StatusSeeOther, "/ui/triggers/"+url.PathEscape(def.ID)+"?flash=Trigger+updated+successfully&flash_kind=success")
}

// handleAPITriggerDelete removes a trigger.
func (w *WebUI) handleAPITriggerDelete(c echo.Context) error {
	id := c.Param("id")
	if w.triggerMgr != nil {
		if err := w.triggerMgr.DeleteTrigger(id); err != nil {
			w.log.Warn().Err(err).Str("trigger", id).Msg("error while deleting trigger file")
		}
	}

	return c.Redirect(http.StatusSeeOther, "/ui/triggers?flash=Trigger+deleted+successfully&flash_kind=success")
}

// handleAPITriggerRun runs a trigger on demand.
func (w *WebUI) handleAPITriggerRun(c echo.Context) error {
	id := c.Param("id")
	if w.triggerMgr == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "Trigger manager not available"})
	}

	eventJSONStr := c.FormValue("event_json")
	var eventData any
	if eventJSONStr != "" {
		_ = json.Unmarshal([]byte(eventJSONStr), &eventData)
	}

	record, err := w.triggerMgr.Run(c.Request().Context(), id, eventData)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{"success": true, "record": record})
}

// handleAPIRuns returns runs and optional details.
func (w *WebUI) handleAPIRuns(c echo.Context) error {
	ctx := c.Request().Context()
	selectedID := c.QueryParam("id")
	triggerFilter := c.QueryParam("trigger")

	runs := make([]store.TriggerRunRecord, 0)
	var selectedRun *store.TriggerRunRecord
	toolCalls := make([]store.ToolCallRecord, 0)

	if w.db != nil {
		if r, err := w.db.ListTriggerRuns(ctx, 50, triggerFilter); err == nil && r != nil {
			runs = r
		}
		if selectedID != "" {
			selectedRun, _ = w.db.GetTriggerRun(ctx, selectedID)
			if selectedRun != nil {
				if tc, err := w.db.ListToolCalls(ctx, 50, selectedID); err == nil && tc != nil {
					toolCalls = tc
				}
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"runs":         runs,
		"selected_run": selectedRun,
		"tool_calls":   toolCalls,
	})
}

// handleAPIIntegrations returns active plugins and MCP servers.
func (w *WebUI) handleAPIIntegrations(c echo.Context) error {
	plugins := make([]map[string]any, 0)
	if w.integ != nil {
		if list := w.integ.List(); list != nil {
			plugins = list
		}
	}

	mcpServers := make([]map[string]any, 0)
	if w.reg != nil {
		statuses := w.reg.ServerStatuses()
		for name, status := range statuses {
			mcpServers = append(mcpServers, map[string]any{
				"name":   name,
				"status": string(status),
			})
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"plugins":     plugins,
		"mcp_servers": mcpServers,
	})
}

// handleAPILogs returns recent log lines.
func (w *WebUI) handleAPILogs(c echo.Context) error {
	level := c.QueryParam("level")
	lines := tailFileFiltered(w.cfg.LogPath(), 200, level)
	return c.JSON(http.StatusOK, map[string]any{
		"lines": lines,
	})
}

// handleAPIConfigGet returns configuration data.
func (w *WebUI) handleAPIConfigGet(c echo.Context) error {
	rawYAML, err := os.ReadFile(w.cfgPath)
	if err != nil && !os.IsNotExist(err) {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to read configuration: " + err.Error()})
	}

	var sc *StructuredConfig
	if len(rawYAML) > 0 {
		var rawMap map[string]any
		if err := yaml.Unmarshal(rawYAML, &rawMap); err == nil {
			sc = buildStructuredConfig(rawMap)
		}
	}
	if sc == nil {
		sc = &StructuredConfig{
			LogLevel: "info",
			Server: ServerPayload{
				ListenAddr: ":3333",
			},
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"raw_yaml":    string(rawYAML),
		"config_path": w.cfgPath,
		"structured":  sc,
	})
}

type ConfigSaveReq struct {
	ConfigJSON string `json:"config_json"`
	YAML       string `json:"yaml"`
}

// handleAPIConfigPost saves configuration.
func (w *WebUI) handleAPIConfigPost(c echo.Context) error {
	if w.cfgPath == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "No config file path configured"})
	}

	bodyBytes, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read body"})
	}

	var req ConfigSaveReq
	var sc StructuredConfig
	var finalYAML []byte

	if len(bodyBytes) > 0 {
		// Try unmarshaling as ConfigSaveReq { "config_json": "...", "yaml": "..." }
		if err := json.Unmarshal(bodyBytes, &req); err == nil && (req.ConfigJSON != "" || req.YAML != "") {
			if req.ConfigJSON != "" {
				if err := json.Unmarshal([]byte(req.ConfigJSON), &sc); err != nil {
					return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid structured data: " + err.Error()})
				}
				outYAML, err := structuredToYAML(&sc)
				if err != nil {
					return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to generate YAML: " + err.Error()})
				}
				finalYAML = outYAML
			} else if req.YAML != "" {
				var tmp any
				if err := yaml.Unmarshal([]byte(req.YAML), &tmp); err != nil {
					return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid YAML: " + err.Error()})
				}
				finalYAML = []byte(req.YAML)
			}
		} else if err := json.Unmarshal(bodyBytes, &sc); err == nil && (sc.LogLevel != "" || sc.DataDir != "") {
			// Direct StructuredConfig JSON payload
			outYAML, err := structuredToYAML(&sc)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to generate YAML: " + err.Error()})
			}
			finalYAML = outYAML
		}
	}

	if len(finalYAML) == 0 {
		// Fallback to form values
		formJSON := c.FormValue("config_json")
		formYAML := c.FormValue("yaml")
		if formJSON != "" {
			if err := json.Unmarshal([]byte(formJSON), &sc); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid structured data: " + err.Error()})
			}
			outYAML, err := structuredToYAML(&sc)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to generate YAML: " + err.Error()})
			}
			finalYAML = outYAML
		} else if formYAML != "" {
			var tmp any
			if err := yaml.Unmarshal([]byte(formYAML), &tmp); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid YAML: " + err.Error()})
			}
			finalYAML = []byte(formYAML)
		} else {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "No configuration data provided"})
		}
	}

	if err := os.MkdirAll(filepath.Dir(w.cfgPath), 0o750); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create directory: " + err.Error()})
	}

	if err := os.WriteFile(w.cfgPath, finalYAML, 0o644); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Save failed: " + err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Configuration saved"})
}
