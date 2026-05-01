// Package supervisor manages the lifecycle of Intents.
package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// Supervisor manages the lifecycle of Intents.
type Supervisor struct {
	tasksDir   string
	reg        *registry.Registry
	runIntent  IntentRunner
	log        zerolog.Logger
	onFinished RunFinishedFunc
	bus        *EventBus

	mu       sync.RWMutex
	rootCtx  context.Context
	intents  map[string]*managedIntent // keyed by intent ID
	failures map[string]error          // keyed by file path, value is error

	fsWatcher     *fsnotify.Watcher
	fsWatchPaths  map[string]int // absolute path -> subscriber refcount
	fsWatchMu     sync.Mutex
	fsWatchCancel context.CancelFunc
}

// EventBus returns the internal event bus for subscribing to notifications.
func (s *Supervisor) EventBus() *EventBus {
	return s.bus
}

type RunFinishedFunc func(ctx context.Context, runID, intentID, status, errorText string)
type IntentRunner func(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
	entrypoint string,
	args any,
) error

type IntentInfo struct {
	ID          string              `json:"id"`
	Path        string              `json:"path,omitempty"`
	Description string              `json:"description,omitempty"`
	Active      bool                `json:"active"`
	Tasks       []orchestrator.Task `json:"tasks,omitempty"`
	Error       string              `json:"error,omitempty"`
}

type managedIntent struct {
	intent       *orchestrator.Intent
	path         string
	cancels      []context.CancelFunc
	active       bool
	activeTasks  int
	started      time.Time
	runSeq       int64
	fswatchPaths []string // absolute paths watched for fs.on_change tasks
}

// New creates a Supervisor.
func New(
	tasksDir string,
	reg *registry.Registry,
	runner IntentRunner,
	log zerolog.Logger,
) *Supervisor {
	fsWatcher, _ := fsnotify.NewWatcher()
	fsCtx, fsCancel := context.WithCancel(context.Background())

	sup := &Supervisor{
		tasksDir:      tasksDir,
		reg:           reg,
		runIntent:     runner,
		log:           log.With().Str("component", "supervisor").Logger(),
		intents:       make(map[string]*managedIntent),
		failures:      make(map[string]error),
		bus:           NewEventBus(),
		rootCtx:       context.Background(),
		fsWatcher:     fsWatcher,
		fsWatchPaths:  make(map[string]int),
		fsWatchCancel: fsCancel,
	}
	reg.Subscribe(func(serverName, method string, params any) {
		normalized := NormalizeNotificationParams(params)
		sup.bus.Publish(Event{
			Server: serverName,
			Method: method,
			Params: normalized,
		})
	})
	if fsWatcher != nil {
		go sup.runFSWatcher(fsCtx)
	}
	return sup
}

func (s *Supervisor) WithOnRunFinished(fn RunFinishedFunc) *Supervisor {
	s.onFinished = fn
	return s
}

// Start blocks until ctx is cancelled.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	s.rootCtx = ctx
	s.mu.Unlock()

	<-ctx.Done()
	s.shutdown()
	return nil
}

// RegisterIntent manually adds an intent to the supervisor's registry.
func (s *Supervisor) RegisterIntent(path string, intent *orchestrator.Intent) error {
	return s.deployIntent(path, intent)
}

// UnregisterIntent removes an intent from the supervisor and stops its tasks.
func (s *Supervisor) UnregisterIntent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	managed, ok := s.intents[id]
	if !ok {
		return errors.Newf("intent %q not found", id)
	}

	for _, cancel := range managed.cancels {
		cancel()
	}
	for _, p := range managed.fswatchPaths {
		s.removeFSWatch(p)
	}
	managed.cancels = nil
	managed.fswatchPaths = nil
	managed.activeTasks = 0
	managed.active = false

	delete(s.intents, id)
	delete(s.failures, managed.path)
	return nil
}

func (s *Supervisor) deployIntent(path string, intent *orchestrator.Intent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanPath := filepath.Clean(path)
	id := intent.ID

	// Check for existing intent with the same ID but from a different file.
	if existing, ok := s.intents[id]; ok && existing.path != cleanPath {
		return errors.Newf("intent %q already defined at %q", id, existing.path)
	}

	// If already deployed from this path, stop tasks so we can re-deploy.
	if managed, ok := s.intents[id]; ok {
		s.log.Info().Str("intent_id", id).Msg("stopping previous intent instance")
		for _, cancel := range managed.cancels {
			cancel()
		}
		for _, p := range managed.fswatchPaths {
			s.removeFSWatch(p)
		}
		managed.cancels = nil
		managed.fswatchPaths = nil
		managed.activeTasks = 0
		managed.active = false
	}

	managed := &managedIntent{
		intent: intent,
		path:   cleanPath,
	}
	s.intents[id] = managed
	delete(s.failures, cleanPath)

	// Activate persistent tasks (schedule, worker, event).
	for _, t := range intent.Tasks {
		if t.Mode == orchestrator.IntentModeOnDemand || t.Mode == "" {
			continue
		}
		// For fs.on_change triggers, expand the path and set up a daemon-level watcher.
		if t.Mode == orchestrator.IntentModeEvent && t.Trigger == "fs.on_change" {
			if path, ok := t.TriggerArgs["path"].(string); ok && path != "" {
				expanded := supervisorExpandPath(path)
				t.TriggerArgs["path"] = expanded
				managed.fswatchPaths = append(managed.fswatchPaths, expanded)
				s.addFSWatch(expanded)
			}
		}
		ctx, cancel := context.WithCancel(s.rootCtx)
		managed.cancels = append(managed.cancels, cancel)
		managed.activeTasks++
		managed.active = true
		go s.runPersistentTask(ctx, managed, t)
	}

	if managed.active {
		managed.started = time.Now()
	}

	s.log.Debug().
		Str("intent_id", id).
		Str("path", cleanPath).
		Int("active_tasks", managed.activeTasks).
		Msg("intent deployed")
	return nil
}

func (s *Supervisor) runPersistentTask(
	ctx context.Context,
	managed *managedIntent,
	task orchestrator.Task,
) {
	id := managed.intent.ID
	runSeq := atomic.LoadInt64(&managed.runSeq)

	defer s.trackTaskFinished(managed, runSeq)

	switch task.Mode {
	case orchestrator.IntentModeSchedule:
		s.log.Debug().
			Str("intent_id", id).
			Str("handler", task.Handler).
			Str("schedule", task.Schedule).
			Msg("starting scheduled task")
		s.loopScheduled(ctx, managed, task, runSeq)
	case orchestrator.IntentModeWorker:
		s.log.Debug().
			Str("intent_id", id).
			Str("handler", task.Handler).
			Str("interval", task.Interval).
			Msg("starting worker task")
		s.loopWorker(ctx, managed, task, runSeq)
	case orchestrator.IntentModeEvent:
		s.log.Debug().
			Str("intent_id", id).
			Str("handler", task.Handler).
			Str("trigger", task.Trigger).
			Msg("starting event-driven task")
		s.loopEvent(ctx, managed, task, runSeq)
	}
}

func (s *Supervisor) loopScheduled(
	ctx context.Context,
	managed *managedIntent,
	task orchestrator.Task,
	runSeq int64,
) {
	// Simple polling scheduler for PoC. In production, use a proper cron lib.
	// For now, only @every durations are supported in the Task.Schedule field.
	interval, err := time.ParseDuration(strings.TrimPrefix(task.Schedule, "@every "))
	if err != nil {
		s.log.Error().
			Err(err).
			Str("intent_id", managed.intent.ID).
			Str("schedule", task.Schedule).
			Msg("invalid schedule duration; task will not run")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.executeTask(ctx, managed, task, runSeq, nil)
		}
	}
}

func (s *Supervisor) loopWorker(
	ctx context.Context,
	managed *managedIntent,
	task orchestrator.Task,
	runSeq int64,
) {
	intervalStr := task.Interval
	if intervalStr == "" {
		intervalStr = "1s"
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		s.log.Error().
			Err(err).
			Str("intent_id", managed.intent.ID).
			Str("interval", intervalStr).
			Msg("invalid worker interval; task will not run")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.executeTask(ctx, managed, task, runSeq, nil)
		}
	}
}

func (s *Supervisor) loopEvent(
	ctx context.Context,
	managed *managedIntent,
	task orchestrator.Task,
	runSeq int64,
) {
	events, stop := s.bus.Subscribe()
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			if !s.matchEvent(ev, task) {
				continue
			}
			s.executeTask(ctx, managed, task, runSeq, ev.Params)
		}
	}
}

func (s *Supervisor) matchEvent(ev Event, task orchestrator.Task) bool {
	if ev.Server != "" && task.Trigger != "" {
		// Event triggers are "server.method" or "method"
		if ev.Server+"."+ev.Method == task.Trigger || ev.Method == task.Trigger {
			return s.matchArgs(ev.Params, task.TriggerArgs)
		}
	}
	return false
}

func (s *Supervisor) matchArgs(params any, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}
	pMap, ok := params.(map[string]any)
	if !ok {
		return false
	}
	for k, v := range filter {
		if !reflect.DeepEqual(pMap[k], v) {
			return false
		}
	}
	return true
}

func (s *Supervisor) executeTask(
	ctx context.Context,
	managed *managedIntent,
	task orchestrator.Task,
	runSeq int64,
	args any,
) {
	runID := fmt.Sprintf("%s-%s-%d", managed.intent.ID, task.Handler, time.Now().UnixNano())
	err := s.runIntent(ctx, managed.intent, runID, task.Handler, args)
	if s.onFinished != nil {
		status := "completed"
		errorText := ""
		if err != nil {
			status = "failed"
			errorText = err.Error()
		}
		s.onFinished(ctx, runID, managed.intent.ID, status, errorText)
	}
}

func (s *Supervisor) trackTaskFinished(managed *managedIntent, runSeq int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if managed.runSeq != runSeq {
		return
	}
	if managed.activeTasks > 0 {
		managed.activeTasks--
	}
	if managed.activeTasks == 0 {
		managed.active = false
		managed.cancels = nil
	}
}

func (s *Supervisor) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, managed := range s.intents {
		s.log.Info().Str("intent_id", id).Msg("stopping intent on shutdown")
		for _, cancel := range managed.cancels {
			cancel()
		}
		managed.cancels = nil
		managed.fswatchPaths = nil
		managed.activeTasks = 0
		managed.active = false
	}

	s.fsWatchCancel() // stops runFSWatcher, which closes the watcher
}

// runFSWatcher reads events from the daemon-level fsnotify watcher and publishes
// them to the event bus as "fs"/"on_change" events. It exits when ctx is done.
func (s *Supervisor) runFSWatcher(ctx context.Context) {
	if s.fsWatcher == nil {
		return
	}
	defer s.fsWatcher.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.fsWatcher.Events:
			if !ok {
				return
			}
			changeType := classifyFSEvent(event)
			if changeType == "" {
				continue
			}
			s.bus.Publish(Event{
				Server: "fs",
				Method: "on_change",
				Params: map[string]any{
					"path":  event.Name,
					"event": changeType,
				},
			})
		case _, ok := <-s.fsWatcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// addFSWatch adds path to the daemon-level watcher, using refcounting so multiple
// tasks watching the same path only register one OS-level watch.
func (s *Supervisor) addFSWatch(path string) {
	if s.fsWatcher == nil {
		return
	}
	s.fsWatchMu.Lock()
	defer s.fsWatchMu.Unlock()
	s.fsWatchPaths[path]++
	if s.fsWatchPaths[path] == 1 {
		if err := s.fsWatcher.Add(path); err != nil {
			s.log.Warn().Err(err).Str("path", path).Msg("failed to add fs.on_change watch")
		}
	}
}

// removeFSWatch decrements the refcount for path and removes the OS-level watch
// when the last subscriber unregisters.
func (s *Supervisor) removeFSWatch(path string) {
	if s.fsWatcher == nil {
		return
	}
	s.fsWatchMu.Lock()
	defer s.fsWatchMu.Unlock()
	s.fsWatchPaths[path]--
	if s.fsWatchPaths[path] <= 0 {
		delete(s.fsWatchPaths, path)
		_ = s.fsWatcher.Remove(path)
	}
}

// classifyFSEvent maps an fsnotify.Event to a human-readable change type.
func classifyFSEvent(event fsnotify.Event) string {
	switch {
	case event.Has(fsnotify.Create):
		return "create"
	case event.Has(fsnotify.Write), event.Has(fsnotify.Chmod), event.Has(fsnotify.Rename):
		return "change"
	case event.Has(fsnotify.Remove):
		return "delete"
	default:
		return ""
	}
}

// supervisorExpandPath expands ~ and environment variables in path.
func supervisorExpandPath(path string) string {
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// ActiveIntents returns a snapshot of currently-deployed intents.
func (s *Supervisor) ActiveIntents() []*orchestrator.Intent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	intents := make([]*orchestrator.Intent, 0, len(s.intents))
	for _, managed := range s.intents {
		intents = append(intents, managed.intent)
	}
	return intents
}

// Intent returns a single intent by ID.
func (s *Supervisor) Intent(id string) (*orchestrator.Intent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	managed, ok := s.intents[id]
	if !ok {
		return nil, false
	}
	return managed.intent, true
}

// StartIntent manually activates a persistent task by name.
func (s *Supervisor) StartIntent(id, taskName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.intents[id]
	if !ok {
		return errors.Newf("intent %q not found", id)
	}
	// TODO: Implement explicit activation if needed for on-demand
	return nil
}

// StopIntent manually deactivates a persistent task by name.
func (s *Supervisor) StopIntent(id, taskName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	managed, ok := s.intents[id]
	if !ok {
		return errors.Newf("intent %q not found", id)
	}
	for _, cancel := range managed.cancels {
		cancel()
	}
	managed.cancels = nil
	managed.activeTasks = 0
	managed.active = false
	return nil
}

// IntentInfos returns a list of information about all deployed intents.
func (s *Supervisor) IntentInfos() []IntentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	infos := make([]IntentInfo, 0, len(s.intents)+len(s.failures))
	for id, managed := range s.intents {
		infos = append(infos, IntentInfo{
			ID:          id,
			Path:        managed.path,
			Description: managed.intent.Description,
			Active:      managed.active,
			Tasks:       managed.intent.Tasks,
		})
	}
	for path, err := range s.failures {
		infos = append(infos, IntentInfo{
			ID:    filepath.Base(path),
			Path:  path,
			Error: err.Error(),
		})
	}
	return infos
}

type SupervisorValidationError struct {
	IntentID  string
	StateName string
	Action    string
}

func (e *SupervisorValidationError) Error() string {
	return fmt.Sprintf(
		"intent %q state %q uses unregistered tool %q",
		e.IntentID,
		e.StateName,
		e.Action,
	)
}

// ValidateIntent checks if an intent is valid within the current registry context.
func (s *Supervisor) ValidateIntent(intent *orchestrator.Intent) error {
	// Starlark and native intents don't use the state machine States map.
	if intent.WorkflowKind() != orchestrator.WorkflowTypeStateMachine {
		return nil
	}

	for name, state := range intent.States {
		if state.Action != "" && !s.reg.Has(state.Action) {
			return &SupervisorValidationError{
				IntentID:  intent.ID,
				StateName: name,
				Action:    state.Action,
			}
		}
	}
	return nil
}

// NormalizeNotificationParams converts opaque MCP SDK param structs
// (e.g. mcp.NotificationParams) to a plain map[string]any via JSON
// round-trip.
func NormalizeNotificationParams(params any) map[string]any {
	if params == nil {
		return nil
	}
	if m, ok := params.(map[string]any); ok {
		return m
	}
	data, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

func atomicAdd64(addr *int64, delta int64) int64 {
	return atomic.AddInt64(addr, delta)
}
