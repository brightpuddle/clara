// Package orchestrator defines the core domain types for Clara's state machine
// engine: Intent, State, and Transition.
package orchestrator

import (
	"encoding/json"
	"time"

	"gopkg.in/yaml.v3"
)

// Tool is the standard interface for any action the interpreter can invoke.
// Implementations wrap MCP tool calls, the Swift bridge, or local SQLite queries.
type Tool func(ctx any, args map[string]any) (any, error)

// Intent is the compiled workflow representation that Clara executes.
// It is the validated runtime form produced from authored intent sources such
// as `.star` files.
type Intent struct {
	ID           string            `json:"id"                       yaml:"id"`
	Description  string            `json:"description,omitempty"    yaml:"description,omitempty"`
	Mode         string            `json:"mode,omitempty"           yaml:"mode,omitempty"`
	Interval     string            `json:"interval,omitempty"       yaml:"interval,omitempty"`
	Schedule     string            `json:"schedule,omitempty"       yaml:"schedule,omitempty"` // cron expression
	Trigger      string            `json:"trigger,omitempty"        yaml:"trigger,omitempty"`  // event expression
	Tasks        []Task            `json:"tasks,omitempty"          yaml:"tasks,omitempty"`
	WorkflowType string            `json:"workflow_type,omitempty"  yaml:"workflow_type,omitempty"`
	Script       string            `json:"script,omitempty"         yaml:"script,omitempty"`
	InitialState string            `json:"initial_state,omitempty"  yaml:"initial_state,omitempty"`
	Context      map[string]string `json:"context,omitempty"        yaml:"context,omitempty"` // alias → mcp:// URI
	States       map[string]State  `json:"states,omitempty"         yaml:"states,omitempty"`
}

// Task is a single execution unit within an Intent.
type Task struct {
	Handler  string `json:"handler"            yaml:"handler"`
	Mode     string `json:"mode"               yaml:"mode"`
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
	Schedule string `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	Trigger  string `json:"trigger,omitempty"  yaml:"trigger,omitempty"`
}

// Validate returns an error if the Intent is structurally invalid.
func (b *Intent) Validate() error {
	if b.ID == "" {
		return &ValidationError{Field: "id", Message: "must not be empty"}
	}

	for i, task := range b.Tasks {
		if err := task.validate(i); err != nil {
			return err
		}
	}

	if err := validateIntentMode(b.Mode); err != nil {
		return err
	}
	if err := validateRuntimeFields(b); err != nil {
		return err
	}
	switch b.WorkflowKind() {
	case WorkflowTypeStateMachine:
		if b.InitialState == "" {
			return &ValidationError{Field: "initial_state", Message: "must not be empty"}
		}
		if len(b.States) == 0 {
			return &ValidationError{Field: "states", Message: "must contain at least one state"}
		}
		if _, ok := b.States[b.InitialState]; !ok {
			return &ValidationError{
				Field:   "initial_state",
				Message: "references state " + b.InitialState + " which does not exist",
			}
		}
		for name, state := range b.States {
			if err := state.validate(name, b.States); err != nil {
				return err
			}
		}
	case WorkflowTypeStarlark:
		if b.Script == "" {
			return &ValidationError{Field: "script", Message: "must not be empty for starlark workflows"}
		}
		if len(b.States) > 0 {
			return &ValidationError{
				Field:   "states",
				Message: "must be empty for starlark workflows",
			}
		}
		if b.InitialState != "" {
			return &ValidationError{
				Field:   "initial_state",
				Message: "must be empty for starlark workflows",
			}
		}
	default:
		return &ValidationError{
			Field:   "workflow_type",
			Message: "must be one of state_machine or starlark",
		}
	}
	return nil
}

const (
	WorkflowTypeStateMachine = "state_machine"
	WorkflowTypeStarlark     = "starlark"

	IntentModeOnDemand = "on_demand"
	IntentModeSchedule = "schedule"
	IntentModeWorker   = "worker"
	IntentModeEvent    = "event"
)

func (t *Task) validate(index int) error {
	if t.Handler == "" {
		return &ValidationError{Field: "tasks[" + itoa(index) + "].handler", Message: "must not be empty"}
	}
	if err := validateIntentMode(t.Mode); err != nil {
		return &ValidationError{Field: "tasks[" + itoa(index) + "].mode", Message: err.(*ValidationError).Message}
	}
	switch t.Mode {
	case IntentModeSchedule:
		if t.Schedule == "" {
			return &ValidationError{Field: "tasks[" + itoa(index) + "].schedule", Message: "must not be empty for schedule mode"}
		}
	case IntentModeWorker:
		if t.Interval == "" {
			return &ValidationError{Field: "tasks[" + itoa(index) + "].interval", Message: "must not be empty for worker mode"}
		}
		if _, err := time.ParseDuration(t.Interval); err != nil {
			return &ValidationError{Field: "tasks[" + itoa(index) + "].interval", Message: "must be a valid duration for worker mode"}
		}
	case IntentModeEvent:
		if t.Trigger == "" {
			return &ValidationError{Field: "tasks[" + itoa(index) + "].trigger", Message: "must not be empty for event mode"}
		}
	}
	return nil
}

func (b *Intent) EffectiveTasks() []Task {
	if len(b.Tasks) > 0 {
		return b.Tasks
	}
	return []Task{{
		Handler:  "main",
		Mode:     b.RuntimeMode(),
		Interval: b.Interval,
		Schedule: b.Schedule,
		Trigger:  b.Trigger,
	}}
}

// WorkflowKind returns the active execution engine for this Intent.
func (b *Intent) WorkflowKind() string {
	switch b.WorkflowType {
	case "", WorkflowTypeStateMachine:
		if b.Script != "" && len(b.States) == 0 {
			return WorkflowTypeStarlark
		}
		return WorkflowTypeStateMachine
	case WorkflowTypeStarlark:
		return WorkflowTypeStarlark
	default:
		return b.WorkflowType
	}
}

func (b *Intent) RuntimeMode() string {
	if b.Mode == "" {
		return IntentModeOnDemand
	}
	return b.Mode
}

func validateIntentMode(mode string) error {
	switch mode {
	case "", IntentModeOnDemand, IntentModeSchedule, IntentModeWorker, IntentModeEvent:
		return nil
	default:
		return &ValidationError{
			Field:   "mode",
			Message: "must be one of on_demand, schedule, worker, or event",
		}
	}
}

func validateRuntimeFields(intent *Intent) error {
	switch intent.RuntimeMode() {
	case IntentModeOnDemand:
		return nil
	case IntentModeSchedule:
		if intent.Schedule == "" {
			return &ValidationError{Field: "schedule", Message: "must not be empty for schedule mode"}
		}
		return nil
	case IntentModeWorker:
		if intent.Interval == "" {
			return &ValidationError{Field: "interval", Message: "must not be empty for worker mode"}
		}
		if _, err := time.ParseDuration(intent.Interval); err != nil {
			return &ValidationError{Field: "interval", Message: "must be a valid duration for worker mode"}
		}
		return nil
	case IntentModeEvent:
		return nil
	default:
		return nil
	}
}

// State is a single node in the Intent execution graph.
type State struct {
	Action      string         `json:"action,omitempty"      yaml:"action,omitempty"`      // maps to a Tool in the Registry
	Args        map[string]any `json:"args,omitempty"        yaml:"args,omitempty"`        // template-injected arguments
	ForEach     string         `json:"for_each,omitempty"    yaml:"for_each,omitempty"`    // expr resolving to a collection to iterate
	Item        string         `json:"item,omitempty"        yaml:"item,omitempty"`        // mem binding name for foreach entries
	Transitions []Transition   `json:"transitions,omitempty" yaml:"transitions,omitempty"` // evaluated in order via expr
	Next        string         `json:"next,omitempty"        yaml:"next,omitempty"`        // default next state
	Terminal    bool           `json:"terminal,omitempty"    yaml:"terminal,omitempty"`    // ends execution when true
}

func (s *State) validate(name string, states map[string]State) error {
	for i, t := range s.Transitions {
		if t.Condition == "" {
			return &ValidationError{
				Field:   "states." + name + ".transitions[" + itoa(i) + "].condition",
				Message: "must not be empty",
			}
		}
		if t.Next == "" {
			return &ValidationError{
				Field:   "states." + name + ".transitions[" + itoa(i) + "].next",
				Message: "must not be empty",
			}
		}
		if _, ok := states[t.Next]; !ok {
			return &ValidationError{
				Field:   "states." + name + ".transitions[" + itoa(i) + "].next",
				Message: "references state " + t.Next + " which does not exist",
			}
		}
	}
	if !s.Terminal && s.Next != "" {
		if _, ok := states[s.Next]; !ok {
			return &ValidationError{
				Field:   "states." + name + ".next",
				Message: "references state " + s.Next + " which does not exist",
			}
		}
	}
	return nil
}

// Transition defines a conditional edge in the state graph.
// The Condition string is evaluated by expr-lang/expr against the current mem map.
type Transition struct {
	Condition string `json:"condition" yaml:"condition"`
	Next      string `json:"next"      yaml:"next"`
}

// ValidationError is returned when an Intent fails structural validation.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "invalid intent field " + e.Field + ": " + e.Message
}

// ParseIntent deserializes a JSON or YAML document into an Intent and validates it.
// It remains useful for tests and internal compiled forms even though authored
// intent files are now expected to be `.star`.
func ParseIntent(data []byte) (*Intent, error) {
	var b Intent
	if err := json.Unmarshal(data, &b); err != nil {
		if yamlErr := yaml.Unmarshal(data, &b); yamlErr != nil {
			return nil, err
		}
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return &b, nil
}

// itoa is a minimal int-to-string helper to avoid importing strconv.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
