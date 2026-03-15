// Package supervisor watches the tasks directory for Starlark intent files,
// validates them, and manages the lifecycle of Intents derived from them.
package supervisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	runIntent  IntentRunner
	log        zerolog.Logger
	onFinished RunFinishedFunc

	mu      sync.RWMutex
	rootCtx context.Context
	intents map[string]*managedIntent // keyed by intent ID
}

type RunFinishedFunc func(ctx context.Context, runID, intentID, status, errorText string)
type IntentRunner func(
	ctx context.Context,
	intent *orchestrator.Intent,
	runID string,
) error

type IntentInfo struct {
	ID          string
	Description string
	Mode        string
	Schedule    string
	Interval    string
	Trigger     string
	Active      bool
}

type managedIntent struct {
	intent  *orchestrator.Intent
	path    string
	cancel  context.CancelFunc
	active  bool
	started time.Time
	runSeq  int64
}

// New creates a Supervisor.
func New(
	tasksDir string,
	reg *registry.Registry,
	runner IntentRunner,
	log zerolog.Logger,
) *Supervisor {
	return &Supervisor{
		tasksDir:  tasksDir,
		reg:       reg,
		runIntent: runner,
		log:       log.With().Str("component", "supervisor").Logger(),
		intents:   make(map[string]*managedIntent),
	}
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

	entries, err := os.ReadDir(s.tasksDir)
	if err != nil {
		return errors.Wrap(err, "read tasks dir")
	}
	for _, entry := range entries {
		if !entry.IsDir() && isIntentFile(entry.Name()) {
			path := filepath.Join(s.tasksDir, entry.Name())
			s.log.Info().Str("path", path).Msg("loading existing task")
			if err := s.processFile(path); err != nil {
				s.log.Error().Err(err).Str("path", path).Msg("failed to process task file")
			}
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.Wrap(err, "create file watcher")
	}
	defer watcher.Close()

	if err := watcher.Add(s.tasksDir); err != nil {
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
			if !isIntentFile(event.Name) {
				continue
			}
			switch {
			case event.Has(fsnotify.Create), event.Has(fsnotify.Write):
				s.log.Info().Str("path", event.Name).Msg("task file changed")
				if err := s.processFile(event.Name); err != nil {
					s.log.Error().Err(err).Str("path", event.Name).Msg("failed to process task")
				}
			case event.Has(fsnotify.Remove), event.Has(fsnotify.Rename):
				s.log.Info().Str("path", event.Name).Msg("task file removed")
				s.removeIntent(event.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			s.log.Error().Err(err).Msg("file watcher error")
		}
	}
}

func (s *Supervisor) processFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "read task file %q", path)
	}

	intent, err := orchestrator.LoadIntentFile(path, content)
	if err != nil {
		return errors.Wrapf(err, "parse task file %q as intent", path)
	}

	if err := s.ValidateIntent(intent); err != nil {
		return errors.Wrapf(err, "invalid intent from %q", path)
	}

	return s.deployIntent(path, intent)
}

func isIntentFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".star")
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

	if existing, ok := s.intents[intent.ID]; ok {
		s.log.Info().Str("intent_id", intent.ID).Msg("stopping previous intent instance")
		if existing.cancel != nil {
			existing.cancel()
		}
	}

	s.intents[intent.ID] = &managedIntent{
		intent: intent,
		path:   filepath.Clean(path),
	}
	if !shouldAutoStart(intent) {
		return nil
	}
	return s.startIntentLocked(intent.ID)
}

func shouldAutoStart(intent *orchestrator.Intent) bool {
	switch intent.RuntimeMode() {
	case orchestrator.IntentModeSchedule, orchestrator.IntentModeWorker, orchestrator.IntentModeEvent:
		return true
	default:
		return false
	}
}

func (s *Supervisor) StartIntent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startIntentLocked(id)
}

func (s *Supervisor) startIntentLocked(id string) error {
	managed, ok := s.intents[id]
	if !ok {
		return errors.Newf("intent %q not found", id)
	}
	if managed.active {
		return nil
	}
	if s.rootCtx == nil {
		return errors.New("supervisor is not running")
	}
	if managed.intent.RuntimeMode() == orchestrator.IntentModeOnDemand {
		return errors.New("on_demand intents use 'clara intent trigger', not 'start'")
	}

	runCtx, cancel := context.WithCancel(s.rootCtx)
	managed.runSeq++
	runSeq := managed.runSeq
	managed.cancel = cancel
	managed.active = true
	managed.started = time.Now()

	switch managed.intent.RuntimeMode() {
	case orchestrator.IntentModeSchedule:
		go s.runScheduledIntent(runCtx, managed.intent, runSeq)
	case orchestrator.IntentModeWorker:
		go s.runWorkerIntent(runCtx, managed.intent, runSeq)
	case orchestrator.IntentModeEvent:
		go s.runEventIntent(runCtx, managed.intent, runSeq)
	default:
		cancel()
		managed.cancel = nil
		managed.active = false
		return errors.Newf("unsupported runtime mode %q", managed.intent.RuntimeMode())
	}
	return nil
}

func (s *Supervisor) StopIntent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	managed, ok := s.intents[id]
	if !ok {
		return errors.Newf("intent %q not found", id)
	}
	if managed.cancel != nil {
		managed.cancel()
	}
	managed.cancel = nil
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

	infos := make([]IntentInfo, 0, len(s.intents))
	for _, managed := range s.intents {
		infos = append(infos, IntentInfo{
			ID:          managed.intent.ID,
			Description: managed.intent.Description,
			Mode:        managed.intent.RuntimeMode(),
			Schedule:    managed.intent.Schedule,
			Interval:    managed.intent.Interval,
			Trigger:     managed.intent.Trigger,
			Active:      managed.active,
		})
	}
	return infos
}

func (s *Supervisor) runScheduledIntent(
	ctx context.Context,
	intent *orchestrator.Intent,
	runSeq int64,
) {
	defer s.markIntentInactive(intent.ID, runSeq)

	for {
		next, err := nextCronTime(intent.Schedule, time.Now())
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

		if err := s.executeManagedRun(ctx, intent); shouldHaltAutoMode(err) {
			return
		}
	}
}

func (s *Supervisor) runWorkerIntent(
	ctx context.Context,
	intent *orchestrator.Intent,
	runSeq int64,
) {
	defer s.markIntentInactive(intent.ID, runSeq)

	interval, err := time.ParseDuration(intent.Interval)
	if err != nil {
		s.log.Error().Err(err).Str("intent_id", intent.ID).Msg("invalid worker interval")
		return
	}
	for {
		if err := s.executeManagedRun(ctx, intent); shouldHaltAutoMode(err) {
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

func (s *Supervisor) runEventIntent(
	ctx context.Context,
	intent *orchestrator.Intent,
	runSeq int64,
) {
	defer s.markIntentInactive(intent.ID, runSeq)
	_ = s.executeManagedRun(ctx, intent)
}

func shouldHaltAutoMode(err error) bool {
	if err == nil {
		return false
	}
	var pauseErr *interpreter.PauseError
	return errors.As(err, &pauseErr)
}

func (s *Supervisor) executeManagedRun(ctx context.Context, intent *orchestrator.Intent) error {
	runID := fmt.Sprintf("%s-%d", intent.ID, time.Now().UnixNano())
	err := s.runIntent(ctx, intent, runID)
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
	managed.active = false
	managed.cancel = nil
}

func (s *Supervisor) removeIntent(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanPath := filepath.Clean(path)
	for id, managed := range s.intents {
		if managed.path != cleanPath {
			continue
		}
		if managed.cancel != nil {
			managed.cancel()
		}
		delete(s.intents, id)
		s.log.Info().Str("intent_id", id).Msg("intent removed")
		return
	}
}

func (s *Supervisor) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, managed := range s.intents {
		s.log.Info().Str("intent_id", id).Msg("stopping intent on shutdown")
		if managed.cancel != nil {
			managed.cancel()
		}
		managed.cancel = nil
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
