// Package sdk defines the Actuator contract for Clara V2.
//
// Every actuator binary imports this package, implements the Actuator interface,
// and calls sdk.Serve() in main(). The Clara daemon loads the binary as a
// hashicorp/go-plugin net/RPC subprocess and communicates over the wire types
// defined here.
package sdk

import (
	"context"
	"time"
)

// Actuator is the interface every actuator binary must implement.
type Actuator interface {
	// Manifest returns the actuator's identity and declared capabilities.
	// Called once at startup by the daemon for CBAC enforcement.
	Manifest() ActuatorManifest

	// Execute is called by the daemon for each routed CloudEvent.
	Execute(ctx context.Context, event Event) (Result, error)
}

// ActuatorManifest describes an actuator's identity and resource capabilities.
type ActuatorManifest struct {
	// ID is the stable, unique identifier for this actuator (e.g. "send-webex-message").
	ID string `json:"id"`

	// Description is a human-readable summary for the LLM evaluator.
	Description string `json:"description"`

	// Capabilities is the list of resource capabilities this actuator requires.
	// Any resource access not declared here is blocked and raises a HITL approval.
	Capabilities []Capability `json:"capabilities"`
}

// Capability declares a resource access permission for CBAC enforcement.
type Capability struct {
	// Resource is the resource path (e.g. "webex:message:send", "fs:*", "shell:exec").
	// Wildcards ("*") are supported as the final segment.
	Resource string `json:"resource"`

	// Description explains why this capability is needed.
	Description string `json:"description"`
}

// Event is the subset of a CloudEvent delivered to an actuator.
type Event struct {
	ID     string         `json:"id"`
	Source string         `json:"source"`
	Type   string         `json:"type"`
	Time   time.Time      `json:"time"`
	Data   map[string]any `json:"data"`
}

// Result is returned by Execute to tell the daemon what happened.
type Result struct {
	// Success indicates whether the actuator completed the action.
	Success bool `json:"success"`

	// Output is a human-readable summary of what was done (logged to hub).
	Output string `json:"output,omitempty"`

	// Retry signals that the daemon should re-queue the event after Delay.
	Retry bool `json:"retry,omitempty"`

	// Delay is the wait duration before a retry (zero means immediate).
	Delay time.Duration `json:"delay,omitempty"`

	// Data is an optional structured payload for chaining or HITL display.
	Data map[string]any `json:"data,omitempty"`
}

// State is a typed helper for per-actuator BoltDB key-value state.
// Actuators receive a State handle in their execution context via StateFromContext.
type State struct {
	// BucketPath is the BoltDB bucket hierarchy for this actuator's namespace.
	BucketPath []string
}
