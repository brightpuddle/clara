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

// Expect declares what "working" means for a trigger; Track I judges runs against it.
type Expect struct {
	// MaxSilence is how long the trigger may go without running before it counts as silent.
	MaxSilence time.Duration `json:"max_silence,omitempty" yaml:"max_silence,omitempty"`
	// MaxDuration is the longest a single run should take.
	MaxDuration time.Duration `json:"max_duration,omitempty" yaml:"max_duration,omitempty"`
	// Outcomes lists acceptable receipt outcomes; any other is flagged. Unset means
	// everything except partial and failed is fine.
	Outcomes []string `json:"outcomes,omitempty" yaml:"outcomes,omitempty"`
	// MinActivity sets lower bounds on receipt counts, e.g. {"counts.seen": 1}.
	MinActivity map[string]int `json:"min_activity,omitempty" yaml:"min_activity,omitempty"`
	// MaxRestartsPerDay bounds worker restarts (each restart is a run) in 24h.
	MaxRestartsPerDay int `json:"max_restarts_per_day,omitempty" yaml:"max_restarts_per_day,omitempty"`
}

// Definition defines a single trigger.
type Definition struct {
	ID          string        `json:"id"                    yaml:"id"`
	Name        string        `json:"name"                  yaml:"name"`
	Description string        `json:"description,omitempty" yaml:"description,omitempty"`
	Enabled     bool          `json:"enabled"               yaml:"enabled"`
	Type        Type          `json:"type"                  yaml:"type"`
	Watch       []string      `json:"watch,omitempty"       yaml:"watch,omitempty"`
	Match       *Rule         `json:"match,omitempty"       yaml:"match,omitempty"`
	Schedule    string        `json:"schedule,omitempty"    yaml:"schedule,omitempty"` // Cron expr or @every 1m
	Debounce    time.Duration `json:"debounce,omitempty"    yaml:"debounce,omitempty"`
	Throttle    time.Duration `json:"throttle,omitempty"    yaml:"throttle,omitempty"`
	Action      Action        `json:"action"                yaml:"action"`
	Expect      *Expect       `json:"expect,omitempty"      yaml:"expect,omitempty"`
}

// ExecutionStatus represents the final status of a run.
type ExecutionStatus string

const (
	StatusSuccess ExecutionStatus = "success"
	StatusFailure ExecutionStatus = "failure"
	StatusTimeout ExecutionStatus = "timeout"
	// StatusCancelled means the supervisor stopped the run (shutdown, reload); not a failure.
	StatusCancelled ExecutionStatus = "cancelled"
	StatusRunning   ExecutionStatus = "running"
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
	EventID     string          `json:"event_id,omitempty"`
	ParentRunID string          `json:"parent_run_id,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	Receipt     *Receipt        `json:"receipt,omitempty"`
}
