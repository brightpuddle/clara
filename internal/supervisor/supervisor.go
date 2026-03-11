// Package supervisor watches the tasks directory for Markdown intent files,
// converts them to Blueprint JSON via an LLM tool, validates them, and manages
// the lifecycle of each Blueprint's execution goroutine.
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
// Markdown intent files to Blueprint JSON.
const LLMTool = "llm.generate_blueprint"

// Supervisor watches a directory for Markdown intent files and manages the
// lifecycle of Blueprints derived from them.
type Supervisor struct {
	tasksDir    string
	reg         *registry.Registry
	interpreter *interpreter.Interpreter
	log         zerolog.Logger

	mu       sync.RWMutex
	blueprints map[string]*managedBlueprint // keyed by blueprint ID
}

type managedBlueprint struct {
	bp     *orchestrator.Blueprint
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
		blueprints:  make(map[string]*managedBlueprint),
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
				s.removeBlueprint(event.Name)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			s.log.Error().Err(err).Msg("file watcher error")
		}
	}
}

// processFile reads a Markdown task file, converts it to a Blueprint via the
// LLM tool, validates it, and starts its execution goroutine.
func (s *Supervisor) processFile(ctx context.Context, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "read task file %q", path)
	}

	bp, err := s.convertToBlueprintWithLLM(ctx, path, string(content))
	if err != nil {
		return errors.Wrapf(err, "convert task file %q to blueprint", path)
	}

	if err := s.ValidateBlueprint(bp); err != nil {
		return errors.Wrapf(err, "invalid blueprint from %q", path)
	}

	s.deployBlueprint(ctx, bp)
	return nil
}

// convertToBlueprintWithLLM calls the LLM tool to convert Markdown to Blueprint JSON.
// Falls back to direct JSON parsing if the content is already valid JSON.
func (s *Supervisor) convertToBlueprintWithLLM(
	ctx context.Context,
	path string,
	content string,
) (*orchestrator.Blueprint, error) {
	// If the content parses directly as Blueprint JSON, use it as-is.
	// This supports manually-authored Blueprint JSON files with a .md extension.
	if bp, err := orchestrator.ParseBlueprint([]byte(content)); err == nil {
		return bp, nil
	}

	tool, ok := s.reg.Get(LLMTool)
	if !ok {
		return nil, errors.Newf("LLM tool %q not registered; cannot convert markdown to blueprint", LLMTool)
	}

	result, err := tool(ctx, map[string]any{
		"intent": content,
		"schema": blueprintSchemaHint,
		"path":   path,
	})
	if err != nil {
		return nil, errors.Wrap(err, "LLM blueprint generation")
	}

	return parseResultAsBlueprint(result)
}

// ValidateBlueprint checks that all referenced tools in a Blueprint are
// registered in the registry. This is called before deployment.
func (s *Supervisor) ValidateBlueprint(bp *orchestrator.Blueprint) error {
	if err := bp.Validate(); err != nil {
		return err
	}
	for stateName, state := range bp.States {
		if state.Action == "" {
			continue
		}
		if !s.reg.Has(state.Action) {
			return &ValidationError{
				BlueprintID: bp.ID,
				StateName:   stateName,
				Action:      state.Action,
			}
		}
	}
	return nil
}

// deployBlueprint cancels any running instance of the same blueprint and
// starts a new goroutine for it.
func (s *Supervisor) deployBlueprint(ctx context.Context, bp *orchestrator.Blueprint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel any existing run of this blueprint.
	if existing, ok := s.blueprints[bp.ID]; ok {
		s.log.Info().Str("blueprint_id", bp.ID).Msg("stopping previous blueprint instance")
		existing.cancel()
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.blueprints[bp.ID] = &managedBlueprint{bp: bp, cancel: cancel}

	s.log.Info().Str("blueprint_id", bp.ID).Msg("starting blueprint")

	wg := conc.NewWaitGroup()
	wg.Go(func() {
		err := s.interpreter.Execute(
			runCtx,
			bp,
			bp.InitialState,
			interpreter.RunOptions{RunID: fmt.Sprintf("%s-%d", bp.ID, now())},
		)
		if err != nil && runCtx.Err() == nil {
			s.log.Error().
				Err(err).
				Str("blueprint_id", bp.ID).
				Msg("blueprint execution error")
		}
	})
	// Detach: the goroutine runs independently and errors are logged above.
	go wg.Wait()
}

func (s *Supervisor) removeBlueprint(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, m := range s.blueprints {
		_ = path // In a full impl, track path→ID mapping.
		m.cancel()
		delete(s.blueprints, id)
		s.log.Info().Str("blueprint_id", id).Msg("blueprint removed")
		break
	}
}

// shutdown cancels all running blueprints.
func (s *Supervisor) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, m := range s.blueprints {
		s.log.Info().Str("blueprint_id", id).Msg("stopping blueprint on shutdown")
		m.cancel()
	}
}

// ActiveBlueprints returns a snapshot of currently-deployed blueprints.
func (s *Supervisor) ActiveBlueprints() []*orchestrator.Blueprint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bps := make([]*orchestrator.Blueprint, 0, len(s.blueprints))
	for _, m := range s.blueprints {
		bps = append(bps, m.bp)
	}
	return bps
}

// parseResultAsBlueprint converts the LLM tool result to a Blueprint.
// The LLM tool should return either a JSON string or a map that represents
// the Blueprint.
func parseResultAsBlueprint(result any) (*orchestrator.Blueprint, error) {
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

	bp, err := orchestrator.ParseBlueprint(jsonBytes)
	if err != nil {
		return nil, errors.Wrap(err, "parse LLM-generated blueprint JSON")
	}
	return bp, nil
}

// now returns the current unix timestamp. Extracted for testability.
var now = func() int64 {
	return time.Now().Unix()
}

// ValidationError is returned when a Blueprint references a tool that is not
// registered in the Registry.
type ValidationError struct {
	BlueprintID string
	StateName   string
	Action      string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf(
		"blueprint %q state %q references unregistered tool %q",
		e.BlueprintID, e.StateName, e.Action,
	)
}

// blueprintSchemaHint is provided to the LLM as context for the expected output format.
const blueprintSchemaHint = `Generate a Clara Blueprint JSON with this structure:
{
  "id": "unique-kebab-case-id",
  "description": "what this blueprint does",
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
