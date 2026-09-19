package trigger

import (
	"time"
)

// Type defines the trigger mechanism.
type Type string

const (
	TypeEvent    Type = "event"
	TypeSchedule Type = "schedule"
	TypeWorker   Type = "worker"
	TypeManual   Type = "manual"
)

// PassEventMode defines how event payload is passed to the script.
type PassEventMode string

const (
	PassEventStdin PassEventMode = "stdin"
	PassEventEnv   PassEventMode = "env"
	PassEventArg   PassEventMode = "arg"
	PassEventNone  PassEventMode = "none"
)

// RestartPolicy defines worker restart behavior on process exit.
type RestartPolicy string

const (
	RestartAlways    RestartPolicy = "always"
	RestartOnFailure RestartPolicy = "on_failure"
	RestartNever     RestartPolicy = "never"
)

// Action defines the executable command and execution parameters.
type Action struct {
	Exec         string            `json:"exec"                    yaml:"exec"`
	Args         []string          `json:"args,omitempty"          yaml:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"           yaml:"env,omitempty"`
	Dir          string            `json:"dir,omitempty"           yaml:"dir,omitempty"`
	PassEvent    PassEventMode     `json:"pass_event,omitempty"    yaml:"pass_event,omitempty"`
	Timeout      time.Duration     `json:"timeout,omitempty"       yaml:"timeout,omitempty"`
	Restart      RestartPolicy     `json:"restart,omitempty"       yaml:"restart,omitempty"`
	RestartDelay time.Duration     `json:"restart_delay,omitempty" yaml:"restart_delay,omitempty"`
	MaxRestarts  int               `json:"max_restarts,omitempty"  yaml:"max_restarts,omitempty"`
}

// Definition defines a single trigger.
type Definition struct {
	ID          string        `json:"id"                    yaml:"id"`
	Name        string        `json:"name"                  yaml:"name"`
	Description string        `json:"description,omitempty" yaml:"description,omitempty"`
	Enabled     bool          `json:"enabled"               yaml:"enabled"`
	Type        Type          `json:"type"                  yaml:"type"`
	Match       *Rule         `json:"match,omitempty"       yaml:"match,omitempty"`
	Schedule    string        `json:"schedule,omitempty"    yaml:"schedule,omitempty"` // Cron expr or @every 1m
	Debounce    time.Duration `json:"debounce,omitempty"    yaml:"debounce,omitempty"`
	Throttle    time.Duration `json:"throttle,omitempty"    yaml:"throttle,omitempty"`
	Action      Action        `json:"action"                yaml:"action"`
}

// ExecutionStatus represents the final status of a run.
type ExecutionStatus string

const (
	StatusSuccess ExecutionStatus = "success"
	StatusFailure ExecutionStatus = "failure"
	StatusTimeout ExecutionStatus = "timeout"
	StatusRunning ExecutionStatus = "running"
)

// RunRecord represents a single execution audit entry.
type RunRecord struct {
	ID          string          `json:"id"`
	TriggerID   string          `json:"trigger_id"`
	TriggerType Type            `json:"trigger_type"`
	Status      ExecutionStatus `json:"status"`
	ExitCode    int             `json:"exit_code"`
	Stdout      string          `json:"stdout"`
	Stderr      string          `json:"stderr"`
	StartedAt   time.Time       `json:"started_at"`
	FinishedAt  time.Time       `json:"finished_at"`
	DurationMs  int64           `json:"duration_ms"`
	Error       string          `json:"error,omitempty"`
	EventData   string          `json:"event_data,omitempty"`
}
