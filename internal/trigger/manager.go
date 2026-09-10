package trigger

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
)

// Manager coordinates event, schedule, and worker triggers.
type Manager struct {
	mu          sync.RWMutex
	triggers    map[string]Definition
	runner      *Runner
	eventBus    *supervisor.EventBus
	store       *store.Store
	cron        *cron.Cron
	cronEntries map[string]cron.EntryID
	workers     map[string]context.CancelFunc
	ctx         context.Context
	cancel      context.CancelFunc
	unsubscribe func()
}

// NewManager creates a new trigger manager.
func NewManager(runner *Runner, eventBus *supervisor.EventBus, st *store.Store) *Manager {
	if runner == nil {
		runner = NewRunner(60 * time.Second)
	}
	return &Manager{
		triggers:    make(map[string]Definition),
		runner:      runner,
		eventBus:    eventBus,
		store:       st,
		cron:        cron.New(cron.WithSeconds()),
		cronEntries: make(map[string]cron.EntryID),
		workers:     make(map[string]context.CancelFunc),
	}
}

// Register adds or updates a trigger definition.
func (m *Manager) Register(def Definition) error {
	if def.ID == "" {
		return errors.New("trigger definition must have an ID")
	}
	if def.Action.Exec == "" {
		return errors.Newf("trigger %q must have an action.exec command", def.ID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop previous worker if replacing
	if cancel, ok := m.workers[def.ID]; ok {
		cancel()
		delete(m.workers, def.ID)
	}

	// Remove previous cron entry if replacing
	if entryID, ok := m.cronEntries[def.ID]; ok {
		m.cron.Remove(entryID)
		delete(m.cronEntries, def.ID)
	}

	m.triggers[def.ID] = def

	// If manager is actively running, schedule/start immediately
	if m.ctx != nil && def.Enabled {
		m.activateTriggerLocked(def)
	}

	return nil
}

// Unregister removes a trigger.
func (m *Manager) Unregister(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, ok := m.workers[id]; ok {
		cancel()
		delete(m.workers, id)
	}
	if entryID, ok := m.cronEntries[id]; ok {
		m.cron.Remove(entryID)
		delete(m.cronEntries, id)
	}
	delete(m.triggers, id)
}

// LoadFromFile loads a single trigger definition or a list of definitions from a YAML file.
func (m *Manager) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "read trigger file: %s", path)
	}

	// Try loading as single definition
	var single Definition
	if err := yaml.Unmarshal(data, &single); err == nil && single.ID != "" && single.Action.Exec != "" {
		return m.Register(single)
	}

	// Try loading as list of definitions
	var list struct {
		Triggers []Definition `yaml:"triggers"`
	}
	if err := yaml.Unmarshal(data, &list); err == nil && len(list.Triggers) > 0 {
		for _, t := range list.Triggers {
			if err := m.Register(t); err != nil {
				return err
			}
		}
		return nil
	}

	return errors.Newf("could not parse trigger definition from %s", path)
}

// LoadFromDir loads all .yaml / .yml files from a directory.
func (m *Manager) LoadFromDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrapf(err, "read trigger dir: %s", dir)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext == ".yaml" || ext == ".yml" {
			fullPath := filepath.Join(dir, entry.Name())
			if err := m.LoadFromFile(fullPath); err != nil {
				log.Warn().Err(err).Str("file", fullPath).Msg("failed to load trigger file")
			}
		}
	}
	return nil
}

// Start begins listening to events, running cron schedules, and launching workers.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.ctx, m.cancel = context.WithCancel(ctx)

	for _, def := range m.triggers {
		if def.Enabled {
			m.activateTriggerLocked(def)
		}
	}
	m.mu.Unlock()

	m.cron.Start()

	if m.eventBus != nil {
		eventCh, unsub := m.eventBus.SubscribeCloud()
		m.unsubscribe = unsub

		go func() {
			for {
				select {
				case <-m.ctx.Done():
					return
				case ce, ok := <-eventCh:
					if !ok {
						return
					}
					m.handleCloudEvent(ce)
				}
			}
		}()
	}

	return nil
}

// Stop gracefully shuts down the trigger manager.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	if m.unsubscribe != nil {
		m.unsubscribe()
	}
	for id, cancel := range m.workers {
		cancel()
		delete(m.workers, id)
	}
	m.cron.Stop()
	m.mu.Unlock()
}

// List returns a snapshot of all registered trigger definitions.
func (m *Manager) List() []Definition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Definition, 0, len(m.triggers))
	for _, t := range m.triggers {
		out = append(out, t)
	}
	return out
}

// Get returns a single trigger definition by ID.
func (m *Manager) Get(id string) (*Definition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.triggers[id]
	if !ok {
		return nil, false
	}
	return &t, true
}

// Run manually triggers an action.
func (m *Manager) Run(ctx context.Context, id string, eventData any) (*RunRecord, error) {
	m.mu.RLock()
	def, ok := m.triggers[id]
	m.mu.RUnlock()

	if !ok {
		return nil, errors.Newf("trigger %q not found", id)
	}

	record := m.runner.Execute(ctx, def, eventData)
	m.recordRun(ctx, record)
	return record, nil
}

// Test tests if an event matches a trigger's matching rule.
func (m *Manager) Test(id string, eventData any) (bool, error) {
	m.mu.RLock()
	def, ok := m.triggers[id]
	m.mu.RUnlock()

	if !ok {
		return false, errors.Newf("trigger %q not found", id)
	}

	if def.Type != TypeEvent || def.Match == nil {
		return true, nil
	}

	return def.Match.Match(eventData)
}

func (m *Manager) activateTriggerLocked(def Definition) {
	switch def.Type {
	case TypeSchedule:
		if def.Schedule != "" {
			entryID, err := m.cron.AddFunc(def.Schedule, func() {
				record := m.runner.Execute(context.Background(), def, map[string]any{
					"source": "clara.schedule",
					"time":   time.Now().Format(time.RFC3339),
				})
				m.recordRun(context.Background(), record)
			})
			if err != nil {
				log.Error().Err(err).Str("trigger", def.ID).Str("schedule", def.Schedule).Msg("failed to register cron schedule")
			} else {
				m.cronEntries[def.ID] = entryID
			}
		}

	case TypeWorker:
		workerCtx, workerCancel := context.WithCancel(m.ctx)
		m.workers[def.ID] = workerCancel
		go m.superviseWorker(workerCtx, def)
	}
}

func (m *Manager) handleCloudEvent(ce supervisor.CloudEvent) {
	m.mu.RLock()
	var matching []Definition
	for _, def := range m.triggers {
		if !def.Enabled || def.Type != TypeEvent {
			continue
		}

		// CloudEvent map representation
		eventMap := map[string]any{
			"id":           ce.ID,
			"source":       ce.Source,
			"type":         ce.Type,
			"time":         ce.Time.Format(time.RFC3339),
			"content_type": ce.ContentType,
			"data":         ce.Data,
		}

		if def.Match == nil {
			matching = append(matching, def)
			continue
		}

		matched, err := def.Match.Match(eventMap)
		if err != nil {
			log.Warn().Err(err).Str("trigger", def.ID).Msg("error matching event rule")
			continue
		}
		if matched {
			matching = append(matching, def)
		}
	}
	m.mu.RUnlock()

	for _, def := range matching {
		targetDef := def
		go func() {
			eventMap := map[string]any{
				"id":           ce.ID,
				"source":       ce.Source,
				"type":         ce.Type,
				"time":         ce.Time.Format(time.RFC3339),
				"content_type": ce.ContentType,
				"data":         ce.Data,
			}
			record := m.runner.Execute(context.Background(), targetDef, eventMap)
			m.recordRun(context.Background(), record)
		}()
	}
}

func (m *Manager) superviseWorker(ctx context.Context, def Definition) {
	restartPolicy := def.Action.Restart
	if restartPolicy == "" {
		restartPolicy = RestartAlways
	}

	delay := def.Action.RestartDelay
	if delay <= 0 {
		delay = 2 * time.Second
	}

	restarts := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		log.Info().Str("worker", def.ID).Str("exec", def.Action.Exec).Msg("starting background worker")
		record := m.runner.Execute(ctx, def, nil)
		m.recordRun(context.Background(), record)

		if ctx.Err() != nil {
			return
		}

		restarts++
		if def.Action.MaxRestarts > 0 && restarts >= def.Action.MaxRestarts {
			log.Warn().Str("worker", def.ID).Int("restarts", restarts).Msg("worker reached max restarts; stopping")
			return
		}

		shouldRestart := false
		switch restartPolicy {
		case RestartAlways:
			shouldRestart = true
		case RestartOnFailure:
			shouldRestart = record.Status != StatusSuccess
		case RestartNever:
			shouldRestart = false
		}

		if !shouldRestart {
			log.Info().Str("worker", def.ID).Msg("worker exited; restart policy satisfied")
			return
		}

		log.Warn().
			Str("worker", def.ID).
			Dur("delay", delay).
			Int("restart_count", restarts).
			Msg("worker terminated; restarting after delay (let it fail)")

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (m *Manager) recordRun(ctx context.Context, r *RunRecord) {
	if m.store == nil || r == nil {
		return
	}

	record := store.TriggerRunRecord{
		ID:          r.ID,
		TriggerID:   r.TriggerID,
		TriggerType: string(r.TriggerType),
		Status:      string(r.Status),
		ExitCode:    r.ExitCode,
		Stdout:      r.Stdout,
		Stderr:      r.Stderr,
		Error:       r.Error,
		EventData:   r.EventData,
		StartedAt:   r.StartedAt,
		FinishedAt:  r.FinishedAt,
		DurationMs:  r.DurationMs,
	}

	if err := m.store.RecordTriggerRun(ctx, record); err != nil {
		log.Error().Err(err).Str("run_id", r.ID).Msg("failed to persist trigger run audit")
	}
}
