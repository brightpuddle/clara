// Package supervisor watches the tasks directory for Markdown intent files,
// converts them to Intent JSON via an LLM tool, validates them, and manages
// the lifecycle of each Intent's execution goroutine.
package supervisor

import (
	"context"
	"encoding/json"
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
	"github.com/sourcegraph/conc"
)

// LLMTool is the name of the registry Tool the supervisor uses to convert
// Markdown intent files to Intent JSON.
const LLMTool = "llm.generate_intent"

// Supervisor watches a directory for Markdown intent files and manages the
// lifecycle of Intents derived from them.
type Supervisor struct {
	tasksDir    string
	reg         *registry.Registry
	interpreter *interpreter.Interpreter
	log         zerolog.Logger

	mu      sync.RWMutex
	intents map[string]*managedIntent // keyed by intent ID
}

type managedIntent struct {
	intent *orchestrator.Intent
	cancel context.CancelFunc
}

// New creates a Supervisor.
func New(
	tasksDir string,
	reg *registry.Registry,
	it *interpreter.Interpreter,
	log zerolog.Logger,
) *Supervisor {
	return &Supervisor{
		tasksDir:    tasksDir,
		reg:         reg,
		interpreter: it,
		log:         log.With().Str("component", "supervisor").Logger(),
		intents:     make(map[string]*managedIntent),
	}
}

// Start watches the tasks directory and blocks until ctx is cancelled.
// Existing .md files are loaded on startup.
func (s *Supervisor) Start(ctx context.Context) error {
	if err := os.MkdirAll(s.tasksDir, 0o750); err != nil {
		return errors.Wrap(err, "create tasks dir")
	}

	// Load existing task files on startup.
	entries, err := os.ReadDir(s.tasksDir)
	if err != nil {
		return errors.Wrap(err, "read tasks dir")
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			path := filepath.Join(s.tasksDir, entry.Name())
			s.log.Info().Str("path", path).Msg("loading existing task")
			if err := s.processFile(ctx, path); err != nil {
				s.log.Error().Err(err).Str("path", path).Msg("failed to process task file")
			}
		}
	}

	// Watch for changes.
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
			if !strings.HasSuffix(event.Name, ".md") {
				continue
			}
			switch {
			case event.Has(fsnotify.Create), event.Has(fsnotify.Write):
				s.log.Info().Str("path", event.Name).Msg("task file changed")
				if err := s.processFile(ctx, event.Name); err != nil {
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

// processFile reads a Markdown task file, converts it to an Intent via the
// LLM tool, validates it, and starts its execution goroutine.
func (s *Supervisor) processFile(ctx context.Context, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "read task file %q", path)
	}

	intent, err := s.convertToIntentWithLLM(ctx, path, string(content))
	if err != nil {
		return errors.Wrapf(err, "convert task file %q to intent", path)
	}

	if err := s.ValidateIntent(intent); err != nil {
		return errors.Wrapf(err, "invalid intent from %q", path)
	}

	s.deployIntent(ctx, intent)
	return nil
}

// convertToIntentWithLLM calls the LLM tool to convert Markdown to Intent JSON.
// Falls back to direct JSON parsing if the content is already valid JSON.
func (s *Supervisor) convertToIntentWithLLM(
	ctx context.Context,
	path string,
	content string,
) (*orchestrator.Intent, error) {
	// If the content parses directly as Intent JSON, use it as-is.
	// This supports manually-authored Intent JSON files with a .md extension.
	if intent, err := orchestrator.ParseIntent([]byte(content)); err == nil {
		return intent, nil
	}

	tool, ok := s.reg.Get(LLMTool)
	if !ok {
		return nil, errors.Newf("LLM tool %q not registered; cannot convert markdown to intent", LLMTool)
	}

	result, err := tool(ctx, map[string]any{
		"intent": content,
		"schema": intentSchemaHint,
		"path":   path,
	})
	if err != nil {
		return nil, errors.Wrap(err, "LLM intent generation")
	}

	return parseResultAsIntent(result)
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

// deployIntent cancels any running instance of the same intent and
// starts a new goroutine for it.
func (s *Supervisor) deployIntent(ctx context.Context, intent *orchestrator.Intent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel any existing run of this intent.
	if existing, ok := s.intents[intent.ID]; ok {
		s.log.Info().Str("intent_id", intent.ID).Msg("stopping previous intent instance")
		existing.cancel()
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.intents[intent.ID] = &managedIntent{intent: intent, cancel: cancel}

	s.log.Info().Str("intent_id", intent.ID).Msg("starting intent")

	wg := conc.NewWaitGroup()
	wg.Go(func() {
		err := s.interpreter.Execute(
			runCtx,
			intent,
			intent.InitialState,
			interpreter.RunOptions{RunID: fmt.Sprintf("%s-%d", intent.ID, now())},
		)
		if err != nil && runCtx.Err() == nil {
			s.log.Error().
				Err(err).
				Str("intent_id", intent.ID).
				Msg("intent execution error")
		}
	})
	// Detach: the goroutine runs independently and errors are logged above.
	go wg.Wait()
}

func (s *Supervisor) removeIntent(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, m := range s.intents {
		_ = path // In a full impl, track path→ID mapping.
		m.cancel()
		delete(s.intents, id)
		s.log.Info().Str("intent_id", id).Msg("intent removed")
		break
	}
}

// shutdown cancels all running intents.
func (s *Supervisor) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, m := range s.intents {
		s.log.Info().Str("intent_id", id).Msg("stopping intent on shutdown")
		m.cancel()
	}
}

// ActiveIntents returns a snapshot of currently-deployed intents.
func (s *Supervisor) ActiveIntents() []*orchestrator.Intent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	intents := make([]*orchestrator.Intent, 0, len(s.intents))
	for _, m := range s.intents {
		intents = append(intents, m.intent)
	}
	return intents
}

// parseResultAsIntent converts the LLM tool result to an Intent.
// The LLM tool should return either a JSON string or a map that represents
// the Intent.
func parseResultAsIntent(result any) (*orchestrator.Intent, error) {
	var jsonBytes []byte
	var err error

	switch v := result.(type) {
	case string:
		jsonBytes = []byte(v)
	case []byte:
		jsonBytes = v
	case map[string]any:
		jsonBytes, err = json.Marshal(v)
		if err != nil {
			return nil, errors.Wrap(err, "marshal LLM result to JSON")
		}
	default:
		return nil, errors.Newf("unexpected LLM result type %T; expected string or map", result)
	}

	intent, err := orchestrator.ParseIntent(jsonBytes)
	if err != nil {
		return nil, errors.Wrap(err, "parse LLM-generated intent JSON")
	}
	return intent, nil
}

// now returns the current unix timestamp. Extracted for testability.
var now = func() int64 {
	return time.Now().Unix()
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

// intentSchemaHint is provided to the LLM as context for the expected output format.
const intentSchemaHint = `Generate a Clara Intent JSON with this structure:
{
  "id": "unique-kebab-case-id",
  "description": "what this intent does",
  "initial_state": "FIRST_STATE",
  "states": {
    "FIRST_STATE": {
      "action": "tool.name",
      "args": {},
      "transitions": [{"condition": "expr bool expression", "next": "NEXT_STATE"}],
      "next": "DEFAULT_NEXT",
      "terminal": false
    },
    "TERMINAL_STATE": {"terminal": true}
  }
}
Return ONLY the JSON object, no explanation.`
