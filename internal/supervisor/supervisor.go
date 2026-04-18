// Package supervisor watches the tasks directory for Starlark intent files,
// validates them, and manages the lifecycle of Intents derived from them.
package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/cockroachdb/errors"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// Supervisor watches a directory for Starlark intent files and manages the
// lifecycle of Intents derived from them.
type Supervisor struct {
	tasksDir   string
	reg        *registry.Registry
	regTimeout time.Duration
	runIntent  IntentRunner
	log        zerolog.Logger
	onFinished RunFinishedFunc
	bus        *EventBus

	mu       sync.RWMutex
	rootCtx  context.Context
	intents  map[string]*managedIntent // keyed by intent ID
	failures map[string]error          // keyed by file path, value is error
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
	ID          string
	Description string
	Active      bool
	Tasks       []orchestrator.Task
	Error       string
}

type managedIntent struct {
	intent      *orchestrator.Intent
	path        string
	cancels     []context.CancelFunc
	active      bool
	activeTasks int
	started     time.Time
	runSeq      int64
}

// New creates a Supervisor.
func New(
	tasksDir string,
	reg *registry.Registry,
	regTimeout time.Duration,
	runner IntentRunner,
	log zerolog.Logger,
) *Supervisor {
	sup := &Supervisor{
		tasksDir:   tasksDir,
		reg:        reg,
		regTimeout: regTimeout,
		runIntent:  runner,
		log:        log.With().Str("component", "supervisor").Logger(),
		intents:    make(map[string]*managedIntent),
		failures:   make(map[string]error),
		bus:        NewEventBus(),
	}
	reg.Subscribe(func(serverName, method string, params any) {
		// Normalize params to map[string]any via JSON round-trip so that
		// opaque MCP SDK structs (e.g. mcp.NotificationParams) are safely
		// converted to plain Go values for the Starlark interpreter.
		normalized := normalizeNotificationParams(params)
		sup.bus.Publish(Event{
			Server: serverName,
			Method: method,
			Params: normalized,
		})
	})
	return sup
}

func (s *Supervisor) WithOnRunFinished(fn RunFinishedFunc) *Supervisor {
	s.onFinished = fn
	return s
}

// Start watches the tasks directory and blocks until ctx is cancelled.
// Existing supported intent files are loaded on startup.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	s.rootCtx = ctx
	s.mu.Unlock()

	if err := os.MkdirAll(s.tasksDir, 0o750); err != nil {
		return errors.Wrap(err, "create tasks dir")
	}

	// Ensure all initial MCP servers have had a chance to connect before we
	// load and start intents.
	if s.regTimeout > 0 {
		waitCtx, cancel := context.WithTimeout(ctx, s.regTimeout)
		defer cancel()
		if err := s.reg.WaitReady(waitCtx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				s.log.Warn().
					Dur("timeout", s.regTimeout).
					Msg("registry did not become fully ready in time; starting with available tools")
			} else if !errors.Is(err, context.Canceled) {
				s.log.Error().Err(err).Msg("failed waiting for registry readiness")
			}
		} else {
			s.log.Info().Msg("registry ready; loading intents")
		}
	}

	if err := s.loadIntentTree(s.tasksDir); err != nil {
		return errors.Wrap(err, "load tasks dir")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.Wrap(err, "create file watcher")
	}
	defer watcher.Close()

	watchedDirs := make(map[string]struct{})
	if err := addWatchTree(watcher, watchedDirs, s.tasksDir); err != nil {
		return errors.Wrap(err, "watch tasks dir")
	}

	s.log.Info().Str("dir", s.tasksDir).Msg("watching tasks directory")

	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			cleanPath := filepath.Clean(event.Name)
			switch {
			case event.Has(fsnotify.Create):
				info, statErr := os.Stat(cleanPath)
				switch {
				case statErr == nil && info.IsDir():
					if err := addWatchTree(watcher, watchedDirs, cleanPath); err != nil {
						s.log.Error().
							Err(err).
							Str("path", cleanPath).
							Msg("failed to watch task dir")
						continue
					}
					if err := s.loadIntentTree(cleanPath); err != nil {
						s.log.Error().Err(err).Str("path", cleanPath).Msg("failed to load task dir")
					}
				case statErr == nil && isIntentFile(cleanPath):
					s.log.Info().Str("path", cleanPath).Msg("task file changed")
					if err := s.processFile(cleanPath); err != nil {
						s.log.Error().Err(err).Str("path", cleanPath).Msg("failed to process task")
					}
				case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
					s.log.Error().
						Err(statErr).
						Str("path", cleanPath).
						Msg("failed to stat task path")
				}
			case event.Has(fsnotify.Write):
				if !isIntentFile(cleanPath) {
					continue
				}
				s.log.Info().Str("path", cleanPath).Msg("task file changed")
				if err := s.processFile(cleanPath); err != nil {
					s.log.Error().Err(err).Str("path", cleanPath).Msg("failed to process task")
				}
			case event.Has(fsnotify.Remove), event.Has(fsnotify.Rename):
				s.log.Info().Str("path", cleanPath).Msg("task path removed")
				removeWatchTree(watcher, watchedDirs, cleanPath, s.log)
				s.removeIntentsUnderPath(cleanPath)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			s.log.Error().Err(err).Msg("file watcher error")
		}
	}
}

func (s *Supervisor) loadIntentTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrapf(err, "walk task path %q", path)
		}
		if d.IsDir() || !isIntentFile(path) {
			return nil
		}
		s.log.Info().Str("path", path).Msg("loading existing task")
		if err := s.processFile(path); err != nil {
			s.log.Error().Err(err).Str("path", path).Msg("failed to process task file")
		}
		return nil
	})
}

func addWatchTree(
	watcher *fsnotify.Watcher,
	watchedDirs map[string]struct{},
	root string,
) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrapf(err, "walk watch path %q", path)
		}
		if !d.IsDir() {
			return nil
		}
		cleanPath := filepath.Clean(path)
		if _, ok := watchedDirs[cleanPath]; ok {
			return nil
		}
		if err := watcher.Add(cleanPath); err != nil {
			return errors.Wrapf(err, "watch path %q", cleanPath)
		}
		watchedDirs[cleanPath] = struct{}{}
		return nil
	})
}

func removeWatchTree(
	watcher *fsnotify.Watcher,
	watchedDirs map[string]struct{},
	root string,
	log zerolog.Logger,
) {
	cleanRoot := filepath.Clean(root)
	prefix := cleanRoot + string(os.PathSeparator)
	for dir := range watchedDirs {
		if dir != cleanRoot && !strings.HasPrefix(dir, prefix) {
			continue
		}
		if err := watcher.Remove(dir); err != nil {
			log.Debug().Err(err).Str("path", dir).Msg("failed to remove task dir watch")
		}
		delete(watchedDirs, dir)
	}
}

func (s *Supervisor) processFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		s.mu.Lock()
		s.failures[filepath.Clean(path)] = err
		s.mu.Unlock()
		return errors.Wrapf(err, "read task file %q", path)
	}

	intent, err := orchestrator.LoadIntentFile(path, content, s.reg.Namespaces())
	if err != nil {
		s.mu.Lock()
		s.failures[filepath.Clean(path)] = err
		s.mu.Unlock()
		return errors.Wrapf(err, "parse task file %q as intent", path)
	}

	if err := s.ValidateIntent(intent); err != nil {
		s.mu.Lock()
		s.failures[filepath.Clean(path)] = err
		s.mu.Unlock()
		return errors.Wrapf(err, "invalid intent from %q", path)
	}

	return s.deployIntent(path, intent)
}

func isIntentFile(path string) bool {
	if !strings.EqualFold(filepath.Ext(path), ".star") {
		return false
	}
	base := strings.ToLower(filepath.Base(path))
	return !strings.HasSuffix(base, "_test.star")
}

// ValidateIntent checks that all referenced tools in an Intent are
// registered in the registry. This is called before deployment.
func (s *Supervisor) ValidateIntent(intent *orchestrator.Intent) error {
	if err := intent.Validate(); err != nil {
		return err
	}
	for stateName, state := range intent.States {
		if state.Action == "" {
			continue
		}
		if !s.reg.Has(state.Action) {
			return &ValidationError{
				IntentID:  intent.ID,
				StateName: stateName,
				Action:    state.Action,
			}
		}
	}
	return nil
}

func (s *Supervisor) deployIntent(path string, intent *orchestrator.Intent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanPath := filepath.Clean(path)
	delete(s.failures, cleanPath)

	// Carry over the runSeq counter from any existing entry so that goroutines
	// from the previous deployment (which captured the old runSeq) cannot
	// accidentally match the new entry's runSeq when they eventually call
	// markIntentInactive.
	var prevRunSeq int64
	if existing, ok := s.intents[intent.ID]; ok {
		s.log.Info().Str("intent_id", intent.ID).Msg("stopping previous intent instance")
		for _, cancel := range existing.cancels {
			cancel()
		}
		prevRunSeq = existing.runSeq
	}

	s.intents[intent.ID] = &managedIntent{
		intent: intent,
		path:   cleanPath,
		runSeq: prevRunSeq, // startIntentLocked will increment before use
	}
	if !shouldAutoStart(intent) {
		return nil
	}
	return s.startIntentLocked(intent.ID, "")
}

func shouldAutoStart(intent *orchestrator.Intent) bool {
	for _, task := range intent.Tasks {
		switch task.Mode {
		case orchestrator.IntentModeSchedule,
			orchestrator.IntentModeWorker,
			orchestrator.IntentModeEvent:
			return true
		}
	}
	return false
}

// StartIntent starts all auto tasks (schedule/worker/event) for the given intent.
// If taskName is non-empty, only the task with that handler name is started.
func (s *Supervisor) StartIntent(id, taskName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startIntentLocked(id, taskName)
}

func (s *Supervisor) startIntentLocked(id, taskName string) error {
	managed, ok := s.intents[id]
	if !ok {
		return errors.Newf("intent %q not found", id)
	}
	if managed.active && taskName == "" {
		return nil
	}
	if s.rootCtx == nil {
		return errors.New("supervisor is not running")
	}

	managed.runSeq++
	runSeq := managed.runSeq
	managed.started = time.Now()
	if taskName == "" {
		managed.activeTasks = 0
		managed.cancels = nil
	}

	for _, task := range managed.intent.Tasks {
		if task.Mode == orchestrator.IntentModeOnDemand {
			continue
		}
		if taskName != "" && task.Handler != taskName {
			continue
		}
		runCtx, cancel := context.WithCancel(s.rootCtx)
		managed.cancels = append(managed.cancels, cancel)
		managed.activeTasks++
		s.runTask(runCtx, managed.intent, task, runSeq)
	}
	managed.active = managed.activeTasks > 0
	return nil
}

func (s *Supervisor) runTask(
	ctx context.Context,
	intent *orchestrator.Intent,
	task orchestrator.Task,
	runSeq int64,
) {
	switch task.Mode {
	case orchestrator.IntentModeSchedule:
		go s.runScheduledTask(ctx, intent, task, runSeq)
	case orchestrator.IntentModeWorker:
		go s.runWorkerTask(ctx, intent, task, runSeq)
	case orchestrator.IntentModeEvent:
		go s.runEventTask(ctx, intent, task, runSeq)
	default:
		s.log.Error().
			Str("intent_id", intent.ID).
			Str("mode", task.Mode).
			Msg("unsupported task mode")
	}
}

// StopIntent stops all running tasks for the given intent.
// If taskName is non-empty, only tasks with that handler name are stopped (best effort).
func (s *Supervisor) StopIntent(id, taskName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	managed, ok := s.intents[id]
	if !ok {
		return errors.Newf("intent %q not found", id)
	}
	// Cancel all goroutines — per-task cancel targeting is not yet supported
	// because the cancels slice is not keyed by handler name.
	for _, cancel := range managed.cancels {
		cancel()
	}
	managed.cancels = nil
	managed.activeTasks = 0
	managed.active = false
	return nil
}

func (s *Supervisor) Intent(id string) (*orchestrator.Intent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	managed, ok := s.intents[id]
	if !ok {
		return nil, false
	}
	return managed.intent, true
}

func (s *Supervisor) IntentInfos() []IntentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	infos := make([]IntentInfo, 0, len(s.intents)+len(s.failures))
	for _, managed := range s.intents {
		infos = append(infos, IntentInfo{
			ID:          managed.intent.ID,
			Description: managed.intent.Description,
			Active:      managed.active,
			Tasks:       managed.intent.Tasks,
		})
	}
	for path, err := range s.failures {
		// Use the filename minus extension as the ID for display purposes.
		id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		infos = append(infos, IntentInfo{
			ID:    id,
			Error: innermostError(err),
		})
	}
	return infos
}

func innermostError(err error) string {
	if err == nil {
		return ""
	}
	return errors.UnwrapAll(err).Error()
}

func (s *Supervisor) runScheduledTask(
	ctx context.Context,
	intent *orchestrator.Intent,
	task orchestrator.Task,
	runSeq int64,
) {
	defer s.markIntentInactive(intent.ID, runSeq)

	for {
		next, err := nextCronTime(task.Schedule, time.Now())
		if err != nil {
			s.log.Error().Err(err).Str("intent_id", intent.ID).Msg("invalid intent schedule")
			return
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if err := s.executeManagedRun(ctx, intent, task.Handler, nil); shouldHaltAutoMode(err) {
			return
		}
	}
}

func (s *Supervisor) runWorkerTask(
	ctx context.Context,
	intent *orchestrator.Intent,
	task orchestrator.Task,
	runSeq int64,
) {
	defer s.markIntentInactive(intent.ID, runSeq)

	interval, err := time.ParseDuration(task.Interval)
	if err != nil {
		s.log.Error().Err(err).Str("intent_id", intent.ID).Msg("invalid worker interval")
		return
	}
	for {
		if err := s.executeManagedRun(ctx, intent, task.Handler, nil); shouldHaltAutoMode(err) {
			return
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Supervisor) runEventTask(
	ctx context.Context,
	intent *orchestrator.Intent,
	task orchestrator.Task,
	runSeq int64,
) {
	defer s.markIntentInactive(intent.ID, runSeq)

	if task.Trigger == "" {
		_ = s.executeManagedRun(ctx, intent, task.Handler, nil)
		return
	}

	// Subscribe to the event bus before activating the watch so we don't miss
	// events that fire between the watch registration and the loop start.
	ch, unsubscribe := s.bus.Subscribe()
	defer unsubscribe()

	// If the trigger server exposes a watch_subscribe tool, register a dynamic
	// watch for this task and clean it up when the task goroutine exits.
	if cleanup := s.subscribeWatch(ctx, task); cleanup != nil {
		defer cleanup()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			// Check if event matches trigger.
			// Format: "server.method"
			fqMethod := event.Server + "." + event.Method
			if fqMethod != task.Trigger {
				continue
			}

			// Check if event params match trigger args.
			params, _ := event.Params.(map[string]any)
			if !matchArgs(task.TriggerArgs, params) {
				continue
			}

			// Triggered!
			s.log.Info().
				Str("intent_id", intent.ID).
				Str("handler", task.Handler).
				Str("trigger", task.Trigger).
				Msg("event trigger matched, starting handler")

			_ = s.executeManagedRun(ctx, intent, task.Handler, event.Params)
		}
	}
}

// subscribeWatch registers a dynamic watch with the trigger server if it
// exposes a "watch_subscribe" tool. It returns a cleanup function that calls
// "watch_unsubscribe" when the event task exits, or nil if no subscription
// was made. This enables ad-hoc file watching driven purely by intent
// definitions — no static watch paths are needed in the daemon config.
func (s *Supervisor) subscribeWatch(ctx context.Context, task orchestrator.Task) func() {
	if len(task.TriggerArgs) == 0 {
		return nil
	}
	// Derive server name from trigger e.g. "fs.on_change" → "fs".
	serverName, _, found := strings.Cut(task.Trigger, ".")
	if !found {
		return nil
	}
	subscribeTool := serverName + ".watch_subscribe"
	if !s.reg.Has(subscribeTool) {
		return nil
	}
	result, err := s.reg.Call(ctx, subscribeTool, task.TriggerArgs)
	if err != nil {
		s.log.Warn().
			Err(err).
			Str("tool", subscribeTool).
			Str("trigger", task.Trigger).
			Msg("dynamic watch subscribe failed")
		return nil
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		return nil
	}
	subID, _ := resultMap["subscription_id"].(string)
	if subID == "" {
		return nil
	}
	s.log.Debug().
		Str("trigger", task.Trigger).
		Str("subscription_id", subID).
		Msg("dynamic watch registered")

	unsubscribeTool := serverName + ".watch_unsubscribe"
	return func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.reg.Call(cleanupCtx, unsubscribeTool, map[string]any{"subscription_id": subID})
		s.log.Debug().
			Str("trigger", task.Trigger).
			Str("subscription_id", subID).
			Msg("dynamic watch unregistered")
	}
}

func matchArgs(expected map[string]any, actual map[string]any) bool {
	for k, v := range expected {
		actualVal, ok := actual[k]
		if !ok {
			return false
		}
		if !reflect.DeepEqual(resolveMatchValue(v), resolveMatchValue(actualVal)) {
			return false
		}
	}
	return true
}

// resolveMatchValue expands ~ and environment variables in string values so
// that trigger args like path="~/.config/foo" match absolute paths in events.
func resolveMatchValue(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	s = os.ExpandEnv(strings.TrimSpace(s))
	if s == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return s
	}
	if strings.HasPrefix(s, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, s[2:])
		}
	}
	return s
}

func shouldHaltAutoMode(err error) bool {
	if err == nil {
		return false
	}
	var pauseErr *interpreter.PauseError
	return errors.As(err, &pauseErr)
}

func (s *Supervisor) executeManagedRun(
	ctx context.Context,
	intent *orchestrator.Intent,
	entrypoint string,
	args any,
) error {
	runID := fmt.Sprintf("%s-%d", intent.ID, time.Now().UnixNano())
	err := s.runIntent(ctx, intent, runID, entrypoint, args)
	if s.onFinished != nil {
		status := "completed"
		errorText := ""
		var pauseErr *interpreter.PauseError
		switch {
		case ctx.Err() != nil:
			status = "cancelled"
		case errors.As(err, &pauseErr):
			status = "waiting"
		case err != nil:
			status = "failed"
			errorText = err.Error()
		}
		s.onFinished(context.WithoutCancel(ctx), runID, intent.ID, status, errorText)
	}
	if err != nil && ctx.Err() == nil {
		s.log.Error().Err(err).Str("intent_id", intent.ID).Msg("intent execution error")
	}
	return err
}

func (s *Supervisor) markIntentInactive(id string, runSeq int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	managed, ok := s.intents[id]
	if !ok {
		return
	}
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

func (s *Supervisor) removeIntentsUnderPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanPath := filepath.Clean(path)
	prefix := cleanPath + string(os.PathSeparator)
	for id, managed := range s.intents {
		if managed.path != cleanPath && !strings.HasPrefix(managed.path, prefix) {
			continue
		}
		for _, cancel := range managed.cancels {
			cancel()
		}
		managed.activeTasks = 0
		delete(s.intents, id)
		s.log.Info().Str("intent_id", id).Str("path", managed.path).Msg("intent removed")
	}

	for p := range s.failures {
		if p == cleanPath || strings.HasPrefix(p, prefix) {
			delete(s.failures, p)
		}
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
		managed.activeTasks = 0
		managed.active = false
	}
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

// ValidationError is returned when an Intent references a tool that is not
// registered in the Registry.
type ValidationError struct {
	IntentID  string
	StateName string
	Action    string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf(
		"intent %q state %q references unregistered tool %q",
		e.IntentID, e.StateName, e.Action,
	)
}

// normalizeNotificationParams converts opaque MCP SDK param structs
// (e.g. mcp.NotificationParams) to a plain map[string]any via JSON
// round-trip so that the Starlark interpreter can consume them safely.
func normalizeNotificationParams(params any) map[string]any {
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
