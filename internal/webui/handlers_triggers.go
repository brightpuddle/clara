package webui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/trigger"
	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
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

func structuredToTrigger(st *StructuredTrigger) (*trigger.Definition, error) {
	def := &trigger.Definition{
		ID:          strings.TrimSpace(st.ID),
		Name:        strings.TrimSpace(st.Name),
		Description: strings.TrimSpace(st.Description),
		Enabled:     st.Enabled,
		Type:        trigger.Type(strings.TrimSpace(st.Type)),
		Action: trigger.Action{
			Exec:      strings.TrimSpace(st.Exec),
			Args:      st.Args,
			Dir:       strings.TrimSpace(st.Dir),
			PassEvent: trigger.PassEventMode(strings.TrimSpace(st.PassEvent)),
		},
	}

	if def.Action.PassEvent == "" {
		def.Action.PassEvent = trigger.PassEventStdin
	}

	// Timeout
	if strings.TrimSpace(st.Timeout) != "" {
		dur, err := parseDurationStr(st.Timeout)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid timeout %q", st.Timeout)
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
