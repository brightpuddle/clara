// Package orchestrator defines the core domain types for Clara's state machine
// engine: Intent, State, and Transition.
package orchestrator

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// Tool is the standard interface for any action the interpreter can invoke.
// Implementations wrap MCP tool calls, the Swift bridge, or local SQLite queries.
type Tool func(ctx any, args map[string]any) (any, error)

// Intent is a declarative state machine that the interpreter executes.
// It is the unit of work in Clara — authored by an LLM from a Markdown intent
// file and validated by the Go daemon before deployment.
type Intent struct {
	ID           string            `json:"id"                       yaml:"id"`
	Description  string            `json:"description,omitempty"    yaml:"description,omitempty"`
	Schedule     string            `json:"schedule,omitempty"       yaml:"schedule,omitempty"` // cron expression
	Trigger      string            `json:"trigger,omitempty"        yaml:"trigger,omitempty"`  // event expression
	WorkflowType string            `json:"workflow_type,omitempty"  yaml:"workflow_type,omitempty"`
	Script       string            `json:"script,omitempty"         yaml:"script,omitempty"`
	InitialState string            `json:"initial_state,omitempty"  yaml:"initial_state,omitempty"`
	Context      map[string]string `json:"context,omitempty"        yaml:"context,omitempty"` // alias → mcp:// URI
	States       map[string]State  `json:"states,omitempty"         yaml:"states,omitempty"`
}

// Validate returns an error if the Intent is structurally invalid.
func (b *Intent) Validate() error {
	if b.ID == "" {
		return &ValidationError{Field: "id", Message: "must not be empty"}
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
)

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
