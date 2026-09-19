package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
)

// StructuredTrigger represents the form-bound trigger fields.
type StructuredTrigger struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Enabled      bool      `json:"enabled"`
	Type         string    `json:"type"`
	Schedule     string    `json:"schedule"`
	Debounce     string    `json:"debounce"`
	Throttle     string    `json:"throttle"`
	RuleField    string    `json:"rule_field"`
	RuleOp       string    `json:"rule_op"`
	RuleValue    string    `json:"rule_value"`
	RuleYAML     string    `json:"rule_yaml"`
	Exec         string    `json:"exec"`
	Args         []string  `json:"args"`
	Dir          string    `json:"dir"`
	PassEvent    string    `json:"pass_event"`
	Timeout      string    `json:"timeout"`
	Restart      string    `json:"restart"`
	RestartDelay string    `json:"restart_delay"`
	MaxRestarts  int       `json:"max_restarts"`
	Env          []EnvPair `json:"env"`
}

type triggerYAML struct {
	ID          string        `yaml:"id"`
	Name        string        `yaml:"name,omitempty"`
	Description string        `yaml:"description,omitempty"`
	Enabled     bool          `yaml:"enabled"`
	Type        trigger.Type  `yaml:"type"`
	Match       *trigger.Rule `yaml:"match,omitempty"`
	Schedule    string        `yaml:"schedule,omitempty"`
	Debounce    string        `yaml:"debounce,omitempty"`
	Throttle    string        `yaml:"throttle,omitempty"`
	Action      actionYAML    `yaml:"action"`
}

type actionYAML struct {
	Exec         string                `yaml:"exec"`
	Args         []string              `yaml:"args,omitempty"`
	Env          map[string]string     `yaml:"env,omitempty"`
	Dir          string                `yaml:"dir,omitempty"`
	PassEvent    trigger.PassEventMode `yaml:"pass_event,omitempty"`
	Timeout      string                `yaml:"timeout,omitempty"`
	Restart      trigger.RestartPolicy `yaml:"restart,omitempty"`
	RestartDelay string                `yaml:"restart_delay,omitempty"`
	MaxRestarts  int                   `yaml:"max_restarts,omitempty"`
}

func (w *WebUI) primaryTriggerDir() string {
	if w.cfg != nil {
		dirs := w.cfg.TriggerDirs()
		if len(dirs) > 0 && dirs[0] != "" {
			return dirs[0]
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clara", "triggers")
}

func (w *WebUI) handleTriggersList(c echo.Context) error {
	ctx := c.Request().Context()
	flash := c.QueryParam("flash")
	flashKind := c.QueryParam("flash_kind")

	var triggers []trigger.Definition
	if w.triggerMgr != nil {
		triggers = w.triggerMgr.List()
	}

	sort.Slice(triggers, func(i, j int) bool {
		return triggers[i].ID < triggers[j].ID
	})

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
		Flash:      flash,
		FlashKind:  flashKind,
	}

	return render(c, http.StatusOK, ui.Triggers(vm))
}

func (w *WebUI) handleTriggerDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	flash := c.QueryParam("flash")
	flashKind := c.QueryParam("flash_kind")

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

	filePath := w.triggerMgr.GetFilePath(id)

	vm := &ui.TriggerDetailVM{
		Base:       w.baseVM("Trigger: "+id, "/ui/triggers"),
		Trigger:    *def,
		RecentRuns: recentRuns,
		Flash:      flash,
		FlashKind:  flashKind,
		FilePath:   filePath,
	}

	return render(c, http.StatusOK, ui.TriggerDetail(vm))
}

func (w *WebUI) handleTriggerNew(c echo.Context) error {
	defaultDef := trigger.Definition{
		ID:      "",
		Name:    "",
		Enabled: true,
		Type:    trigger.TypeEvent,
		Match: &trigger.Rule{
			Field: "type",
			Op:    trigger.OpEquals,
			Value: "custom.event",
		},
		Action: trigger.Action{
			Exec:      "lua scripts/handle_event.lua",
			PassEvent: trigger.PassEventStdin,
			Timeout:   30 * time.Second,
		},
	}

	st := triggerToStructured(defaultDef)
	st.ID = ""
	jsonBytes, _ := json.Marshal(st)
	yamlStr, _ := triggerToYAML(defaultDef)

	vm := &ui.TriggerEditVM{
		Base:        w.baseVM("New Trigger", "/ui/triggers"),
		Trigger:     defaultDef,
		IsNew:       true,
		TriggerJSON: string(jsonBytes),
		YAML:        yamlStr,
	}

	return render(c, http.StatusOK, ui.TriggerEdit(vm))
}

func (w *WebUI) handleTriggerCreate(c echo.Context) error {
	if w.triggerMgr == nil {
		return c.String(http.StatusServiceUnavailable, "Trigger manager not available")
	}

	rawJSON := c.FormValue("trigger_json")
	rawYAML := c.FormValue("yaml")

	var def *trigger.Definition
	var err error

	if rawJSON != "" {
		var st StructuredTrigger
		if err = json.Unmarshal([]byte(rawJSON), &st); err == nil {
			def, err = structuredToTrigger(&st)
		}
	} else if rawYAML != "" {
		def, err = yamlToTrigger(rawYAML)
	} else {
		err = errors.New("no trigger data provided")
	}

	if err != nil {
		return w.renderTriggerEditError(c, nil, true, rawJSON, rawYAML, "Invalid trigger data: "+err.Error())
	}

	if def.ID == "" {
		return w.renderTriggerEditError(c, def, true, rawJSON, rawYAML, "Trigger ID is required")
	}
	if def.Action.Exec == "" {
		return w.renderTriggerEditError(c, def, true, rawJSON, rawYAML, "Executable command (action.exec) is required")
	}

	if _, exists := w.triggerMgr.Get(def.ID); exists {
		return w.renderTriggerEditError(c, def, true, rawJSON, rawYAML, fmt.Sprintf("Trigger %q already exists", def.ID))
	}

	if _, err := w.triggerMgr.SaveToFile(*def, w.primaryTriggerDir()); err != nil {
		return w.renderTriggerEditError(c, def, true, rawJSON, rawYAML, "Failed to save trigger: "+err.Error())
	}

	return c.Redirect(http.StatusSeeOther, "/ui/triggers/"+url.PathEscape(def.ID)+"?flash=Trigger+created+successfully&flash_kind=success")
}

func (w *WebUI) handleTriggerEdit(c echo.Context) error {
	id := c.Param("id")
	if w.triggerMgr == nil {
		return c.String(http.StatusServiceUnavailable, "Trigger manager not available")
	}

	def, ok := w.triggerMgr.Get(id)
	if !ok {
		return c.String(http.StatusNotFound, "Trigger not found")
	}

	st := triggerToStructured(*def)
	jsonBytes, _ := json.Marshal(st)
	yamlStr, _ := triggerToYAML(*def)

	vm := &ui.TriggerEditVM{
		Base:        w.baseVM("Edit Trigger: "+id, "/ui/triggers"),
		Trigger:     *def,
		IsNew:       false,
		TriggerJSON: string(jsonBytes),
		YAML:        yamlStr,
	}

	return render(c, http.StatusOK, ui.TriggerEdit(vm))
}

func (w *WebUI) handleTriggerUpdate(c echo.Context) error {
	id := c.Param("id")
	if w.triggerMgr == nil {
		return c.String(http.StatusServiceUnavailable, "Trigger manager not available")
	}

	rawJSON := c.FormValue("trigger_json")
	rawYAML := c.FormValue("yaml")

	var def *trigger.Definition
	var err error

	if rawJSON != "" {
		var st StructuredTrigger
		if err = json.Unmarshal([]byte(rawJSON), &st); err == nil {
			def, err = structuredToTrigger(&st)
		}
	} else if rawYAML != "" {
		def, err = yamlToTrigger(rawYAML)
	} else {
		err = errors.New("no trigger data provided")
	}

	if err != nil {
		return w.renderTriggerEditError(c, nil, false, rawJSON, rawYAML, "Invalid trigger data: "+err.Error())
	}

	if def.ID == "" {
		return w.renderTriggerEditError(c, def, false, rawJSON, rawYAML, "Trigger ID is required")
	}
	if def.Action.Exec == "" {
		return w.renderTriggerEditError(c, def, false, rawJSON, rawYAML, "Executable command (action.exec) is required")
	}

	// If ID changed, ensure target ID doesn't collide with existing different trigger
	if def.ID != id {
		if _, exists := w.triggerMgr.Get(def.ID); exists {
			return w.renderTriggerEditError(c, def, false, rawJSON, rawYAML, fmt.Sprintf("Trigger ID %q already exists", def.ID))
		}
		_ = w.triggerMgr.DeleteTrigger(id)
	}

	if _, err := w.triggerMgr.SaveToFile(*def, w.primaryTriggerDir()); err != nil {
		return w.renderTriggerEditError(c, def, false, rawJSON, rawYAML, "Failed to save trigger: "+err.Error())
	}

	return c.Redirect(http.StatusSeeOther, "/ui/triggers/"+url.PathEscape(def.ID)+"?flash=Trigger+updated+successfully&flash_kind=success")
}

func (w *WebUI) handleTriggerDelete(c echo.Context) error {
	id := c.Param("id")
	if w.triggerMgr != nil {
		if err := w.triggerMgr.DeleteTrigger(id); err != nil {
			w.log.Warn().Err(err).Str("trigger", id).Msg("error while deleting trigger file")
		}
	}

	return c.Redirect(http.StatusSeeOther, "/ui/triggers?flash=Trigger+deleted+successfully&flash_kind=success")
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

	filePath := w.triggerMgr.GetFilePath(id)

	vm := &ui.TriggerDetailVM{
		Base:       w.baseVM("Trigger: "+id, "/ui/triggers"),
		Trigger:    *def,
		RecentRuns: recentRuns,
		RunResult:  record,
		FilePath:   filePath,
	}

	return render(c, http.StatusOK, ui.TriggerDetail(vm))
}

func (w *WebUI) renderTriggerEditError(c echo.Context, def *trigger.Definition, isNew bool, rawJSON, rawYAML, errMsg string) error {
	var triggerDef trigger.Definition
	if def != nil {
		triggerDef = *def
	}

	title := "Edit Trigger"
	if isNew {
		title = "New Trigger"
	}

	vm := &ui.TriggerEditVM{
		Base:        w.baseVM(title, "/ui/triggers"),
		Trigger:     triggerDef,
		IsNew:       isNew,
		TriggerJSON: rawJSON,
		YAML:        rawYAML,
		Flash:       errMsg,
		FlashKind:   "error",
	}

	return render(c, http.StatusOK, ui.TriggerEdit(vm))
}

func triggerToStructured(def trigger.Definition) *StructuredTrigger {
	st := &StructuredTrigger{
		ID:          def.ID,
		Name:        def.Name,
		Description: def.Description,
		Enabled:     def.Enabled,
		Type:        string(def.Type),
		Schedule:    def.Schedule,
		Exec:        def.Action.Exec,
		Args:        def.Action.Args,
		Dir:         def.Action.Dir,
		PassEvent:   string(def.Action.PassEvent),
		Restart:     string(def.Action.Restart),
		MaxRestarts: def.Action.MaxRestarts,
		Env:         []EnvPair{},
	}

	if st.Args == nil {
		st.Args = []string{}
	}
	if st.Type == "" {
		st.Type = string(trigger.TypeEvent)
	}
	if st.PassEvent == "" {
		st.PassEvent = string(trigger.PassEventStdin)
	}
	if st.Restart == "" {
		st.Restart = string(trigger.RestartAlways)
	}

	if def.Action.Timeout > 0 {
		st.Timeout = def.Action.Timeout.String()
	}
	if def.Action.RestartDelay > 0 {
		st.RestartDelay = def.Action.RestartDelay.String()
	}
	if def.Debounce > 0 {
		st.Debounce = def.Debounce.String()
	}
	if def.Throttle > 0 {
		st.Throttle = def.Throttle.String()
	}

	if def.Match != nil {
		if def.Match.Field != "" && len(def.Match.And) == 0 && len(def.Match.Or) == 0 && def.Match.Not == nil {
			st.RuleField = def.Match.Field
			st.RuleOp = string(def.Match.Op)
			st.RuleValue = fmt.Sprintf("%v", def.Match.Value)
		} else {
			b, _ := yaml.Marshal(def.Match)
			st.RuleYAML = strings.TrimSpace(string(b))
		}
	}

	if len(def.Action.Env) > 0 {
		var keys []string
		for k := range def.Action.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			st.Env = append(st.Env, EnvPair{Key: k, Value: def.Action.Env[k]})
		}
	}

	return st
}

func structuredToTrigger(st *StructuredTrigger) (*trigger.Definition, error) {
	def := &trigger.Definition{
		ID:          strings.TrimSpace(st.ID),
		Name:        strings.TrimSpace(st.Name),
		Description: strings.TrimSpace(st.Description),
		Enabled:     st.Enabled,
		Type:        trigger.Type(strings.TrimSpace(st.Type)),
		Action: trigger.Action{
			Exec:      strings.TrimSpace(st.Exec),
			Dir:       strings.TrimSpace(st.Dir),
			PassEvent: trigger.PassEventMode(strings.TrimSpace(st.PassEvent)),
		},
	}

	if def.Type == "" {
		def.Type = trigger.TypeEvent
	}

	// Args
	for _, arg := range st.Args {
		trimmed := strings.TrimSpace(arg)
		if trimmed != "" {
			def.Action.Args = append(def.Action.Args, trimmed)
		}
	}

	// Timeout
	if strings.TrimSpace(st.Timeout) != "" {
		dur, err := parseDurationStr(st.Timeout)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid action timeout %q", st.Timeout)
		}
		def.Action.Timeout = dur
	}

	// Debounce
	if strings.TrimSpace(st.Debounce) != "" {
		dur, err := parseDurationStr(st.Debounce)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid debounce %q", st.Debounce)
		}
		def.Debounce = dur
	}

	// Throttle
	if strings.TrimSpace(st.Throttle) != "" {
		dur, err := parseDurationStr(st.Throttle)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid throttle %q", st.Throttle)
		}
		def.Throttle = dur
	}

	// Env
	if len(st.Env) > 0 {
		def.Action.Env = make(map[string]string)
		for _, pair := range st.Env {
			k := strings.TrimSpace(pair.Key)
			if k != "" {
				def.Action.Env[k] = pair.Value
			}
		}
	}

	switch def.Type {
	case trigger.TypeSchedule:
		def.Schedule = strings.TrimSpace(st.Schedule)
		if def.Schedule == "" {
			return nil, errors.New("schedule expression is required for schedule trigger")
		}

	case trigger.TypeWorker:
		def.Action.Restart = trigger.RestartPolicy(strings.TrimSpace(st.Restart))
		if def.Action.Restart == "" {
			def.Action.Restart = trigger.RestartAlways
		}
		if strings.TrimSpace(st.RestartDelay) != "" {
			dur, err := parseDurationStr(st.RestartDelay)
			if err != nil {
				return nil, errors.Wrapf(err, "invalid restart delay %q", st.RestartDelay)
			}
			def.Action.RestartDelay = dur
		}
		def.Action.MaxRestarts = st.MaxRestarts

	case trigger.TypeEvent:
		if strings.TrimSpace(st.RuleYAML) != "" {
			var r trigger.Rule
			if err := yaml.Unmarshal([]byte(st.RuleYAML), &r); err != nil {
				return nil, errors.Wrap(err, "invalid rule YAML")
			}
			def.Match = &r
		} else if strings.TrimSpace(st.RuleField) != "" {
			op := trigger.Op(strings.TrimSpace(st.RuleOp))
			if op == "" {
				op = trigger.OpEquals
			}
			def.Match = &trigger.Rule{
				Field: strings.TrimSpace(st.RuleField),
				Op:    op,
				Value: parseRuleValue(st.RuleValue),
			}
		}

	case trigger.TypeManual:
		// Manual triggers don't require schedule expressions or match conditions
	}

	return def, nil
}

func triggerToYAML(def trigger.Definition) (string, error) {
	y := triggerYAML{
		ID:          def.ID,
		Name:        def.Name,
		Description: def.Description,
		Enabled:     def.Enabled,
		Type:        def.Type,
		Match:       def.Match,
		Schedule:    def.Schedule,
		Action: actionYAML{
			Exec:        def.Action.Exec,
			Args:        def.Action.Args,
			Env:         def.Action.Env,
			Dir:         def.Action.Dir,
			PassEvent:   def.Action.PassEvent,
			Restart:     def.Action.Restart,
			MaxRestarts: def.Action.MaxRestarts,
		},
	}
	if def.Action.Timeout > 0 {
		y.Action.Timeout = def.Action.Timeout.String()
	}
	if def.Action.RestartDelay > 0 {
		y.Action.RestartDelay = def.Action.RestartDelay.String()
	}
	if def.Debounce > 0 {
		y.Debounce = def.Debounce.String()
	}
	if def.Throttle > 0 {
		y.Throttle = def.Throttle.String()
	}
	b, err := yaml.Marshal(y)
	if err != nil {
		return "", errors.Wrap(err, "marshal trigger YAML")
	}
	return string(b), nil
}

func yamlToTrigger(yamlStr string) (*trigger.Definition, error) {
	var y triggerYAML
	if err := yaml.Unmarshal([]byte(yamlStr), &y); err != nil {
		return nil, errors.Wrap(err, "parse trigger YAML")
	}
	def := trigger.Definition{
		ID:          y.ID,
		Name:        y.Name,
		Description: y.Description,
		Enabled:     y.Enabled,
		Type:        y.Type,
		Match:       y.Match,
		Schedule:    y.Schedule,
		Action: trigger.Action{
			Exec:        y.Action.Exec,
			Args:        y.Action.Args,
			Env:         y.Action.Env,
			Dir:         y.Action.Dir,
			PassEvent:   y.Action.PassEvent,
			Restart:     y.Action.Restart,
			MaxRestarts: y.Action.MaxRestarts,
		},
	}
	if y.Action.Timeout != "" {
		if dur, err := parseDurationStr(y.Action.Timeout); err == nil {
			def.Action.Timeout = dur
		}
	}
	if y.Action.RestartDelay != "" {
		if dur, err := parseDurationStr(y.Action.RestartDelay); err == nil {
			def.Action.RestartDelay = dur
		}
	}
	if y.Debounce != "" {
		if dur, err := parseDurationStr(y.Debounce); err == nil {
			def.Debounce = dur
		}
	}
	if y.Throttle != "" {
		if dur, err := parseDurationStr(y.Throttle); err == nil {
			def.Throttle = dur
		}
	}
	return &def, nil
}

func parseDurationStr(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return time.ParseDuration(s)
}

func parseRuleValue(s string) any {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "true") {
		return true
	}
	if strings.EqualFold(s, "false") {
		return false
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil && !strings.Contains(s, " ") {
		if !strings.Contains(s, ".") {
			if i, err := strconv.Atoi(s); err == nil {
				return i
			}
		}
		return n
	}
	return s
}
