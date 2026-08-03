package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hashicorp/go-plugin"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/loghub"
	"github.com/brightpuddle/clara/pkg/sdk"
)

// LLMClient represents the interface to execute evaluator queries against the LLM kernel.
type LLMClient interface {
	AnalyzeEvent(ctx context.Context, ev CloudEvent, history []string) (AnalysisResult, error)
	RefineCode(
		ctx context.Context,
		compilerError string,
		failedCode map[string]string,
	) (AnalysisResult, error)
}

// PatchInstruction describes a single Aider-style search/replace edit for an existing actuator file.
// The LLM can return a list of these instead of re-emitting entire files, saving tokens and
// reducing the blast-radius of generated code changes.
type PatchInstruction struct {
	File    string `json:"file"`    // Relative filename inside the actuator workspace subdirectory
	Search  string `json:"search"`  // Exact block to locate (must appear exactly once in the file)
	Replace string `json:"replace"` // Block to substitute in place of Search
}

// AnalysisResult represents the decision of the LLM Evaluator.
type AnalysisResult struct {
	Action       string             `json:"action"`                  // "invoke", "build", "patch", "ignore"
	ActuatorID   string             `json:"actuator_id"`             // ID of the actuator to run or compile
	ProposedCode map[string]string  `json:"proposed_code,omitempty"` // Code to build on "build" action (filename -> content)
	Patches      []PatchInstruction `json:"patches,omitempty"`       // Targeted edits on "patch" action
	HeuristicTTL time.Duration      `json:"heuristic_ttl,omitempty"` // How long to cache this routing decision in the fast-path
}

// HeuristicRule defines a fast-path routing rule with predicate matching and TTL control.
type HeuristicRule struct {
	ID           string        `json:"id"`
	EventType    string        `json:"event_type"`              // e.g. "clara.request", "webex.message", "fs.modify", "*"
	SourcePattern string       `json:"source_pattern,omitempty"` // glob/prefix match on CloudEvent.Source (e.g. "integrations/webex/*")
	PayloadMatch string        `json:"payload_match,omitempty"`  // key=value match on CloudEvent.Data (e.g. "room_id=Y2lz...")
	ActuatorID   string        `json:"actuator_id"`
	TTL          time.Duration `json:"ttl"`                      // 0 = no cache (always force LLM evaluation)
	ExpiresAt    time.Time     `json:"expires_at"`
}

// Matches returns true if the incoming CloudEvent satisfies the HeuristicRule conditions.
func (r HeuristicRule) Matches(ev CloudEvent) bool {
	// EventType matching (supports "*" wildcard)
	if r.EventType != "*" && r.EventType != "" && r.EventType != ev.Type {
		return false
	}

	// Source matching (glob pattern or prefix match)
	if r.SourcePattern != "" {
		matched, err := filepath.Match(r.SourcePattern, ev.Source)
		if err != nil || !matched {
			if !strings.HasPrefix(ev.Source, strings.TrimSuffix(r.SourcePattern, "*")) {
				return false
			}
		}
	}

	// Payload matching ("key=value" string match in ev.Data)
	if r.PayloadMatch != "" {
		parts := strings.SplitN(r.PayloadMatch, "=", 2)
		if len(parts) == 2 {
			k, v := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			if ev.Data == nil {
				return false
			}
			val, ok := ev.Data[k]
			if !ok || fmt.Sprintf("%v", val) != v {
				return false
			}
		}
	}

	return true
}

// Evaluator manages fast-path routing and triggers dynamic self-modification loops in Clara V2.
type Evaluator struct {
	log     zerolog.Logger
	hub     *loghub.Hub
	bus     *EventBus
	llm     LLMClient
	builder *Builder
	binDir  string // directory containing actuator binaries

	mu            sync.RWMutex
	rules         []HeuristicRule // fast-path heuristic matching rules
	manifestCache map[string]sdk.ActuatorManifest
}

// NewEvaluator creates a new Evaluator.
// binDir is the directory where actuator binaries live (e.g. ~/.local/share/clara/bin).
func NewEvaluator(
	log zerolog.Logger,
	hub *loghub.Hub,
	bus *EventBus,
	llm LLMClient,
	builder *Builder,
	binDir string,
) *Evaluator {
	if binDir == "" {
		home, _ := os.UserHomeDir()
		binDir = filepath.Join(home, ".local", "share", "clara", "bin")
	}
	return &Evaluator{
		log:           log.With().Str("component", "evaluator").Logger(),
		hub:           hub,
		bus:           bus,
		llm:           llm,
		builder:       builder,
		binDir:        binDir,
		rules:         make([]HeuristicRule, 0),
		manifestCache: make(map[string]sdk.ActuatorManifest),
	}
}

// RegisterHeuristic manually registers a fast-path routing rule.
func (e *Evaluator) RegisterHeuristic(eventType string, actuatorID string, ttl time.Duration) {
	e.RegisterRule(HeuristicRule{
		ID:         fmt.Sprintf("rule-%s-%d", eventType, time.Now().UnixNano()),
		EventType:  eventType,
		ActuatorID: actuatorID,
		TTL:        ttl,
	})
}

// RegisterRule adds a detailed match-specific HeuristicRule to the fast-path routing engine.
// If TTL == 0, the rule is not stored in the fast-path cache.
func (e *Evaluator) RegisterRule(rule HeuristicRule) {
	if rule.TTL <= 0 {
		e.log.Debug().
			Str("type", rule.EventType).
			Str("actuator", rule.ActuatorID).
			Msg("skipping fast-path registration for rule with TTL=0 (uncached)")
		return
	}

	rule.ExpiresAt = time.Now().Add(rule.TTL)

	e.mu.Lock()
	defer e.mu.Unlock()

	// Update existing rule matching ID or EventType+SourcePattern+PayloadMatch, else append
	updated := false
	for i, r := range e.rules {
		if (rule.ID != "" && r.ID == rule.ID) ||
			(r.EventType == rule.EventType && r.SourcePattern == rule.SourcePattern && r.PayloadMatch == rule.PayloadMatch) {
			e.rules[i] = rule
			updated = true
			break
		}
	}
	if !updated {
		e.rules = append(e.rules, rule)
	}

	e.log.Debug().
		Str("id", rule.ID).
		Str("type", rule.EventType).
		Str("actuator", rule.ActuatorID).
		Dur("ttl", rule.TTL).
		Msg("registered fast-path heuristic rule")
}

// ListRules returns all active (non-expired) heuristic rules.
func (e *Evaluator) ListRules() []HeuristicRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	now := time.Now()
	active := make([]HeuristicRule, 0, len(e.rules))
	for _, r := range e.rules {
		if now.Before(r.ExpiresAt) {
			active = append(active, r)
		}
	}
	return active
}

// pushEval publishes an evaluator decision to the log hub (no-op if hub is nil).
func (e *Evaluator) pushEval(level, msg string, fields map[string]any) {
	if e.hub != nil {
		e.hub.PushEvaluator(level, msg, fields)
	}
}

// OnEvent processes an incoming event. It prioritizes fast-path heuristics and falls back to LLM analysis.
func (e *Evaluator) OnEvent(ctx context.Context, ev CloudEvent) error {
	e.log.Info().Str("event_id", ev.ID).Str("type", ev.Type).Msg("processing ingress event")
	e.pushEval("info", "processing ingress event", map[string]any{
		"event_id": ev.ID,
		"type":     ev.Type,
		"source":   ev.Source,
	})

	// Direct route for manual actuator runs (clara actuator run)
	if ev.Type == "clara.actuator.run" {
		actuatorID, _ := ev.Data["actuator_id"].(string)
		if actuatorID == "" {
			return errors.New("missing actuator_id in clara.actuator.run event data")
		}
		e.log.Info().Str("actuator", actuatorID).Msg("manual actuator run triggered via event")
		return e.executeActuator(ctx, actuatorID, ev)
	}

	// 1. Fast-Path Heuristic Rule Check
	// Interactive requests (clara.request, clara.prompt) always bypass the heuristic cache (TTL=0)
	if ev.Type != "clara.request" && ev.Type != "clara.prompt" {
		now := time.Now()
		e.mu.RLock()
		var matchedRule *HeuristicRule
		for _, r := range e.rules {
			if now.Before(r.ExpiresAt) && r.Matches(ev) {
				ruleCopy := r
				matchedRule = &ruleCopy
				break
			}
		}
		e.mu.RUnlock()

		if matchedRule != nil {
			e.log.Info().
				Str("rule_id", matchedRule.ID).
				Str("type", ev.Type).
				Str("actuator", matchedRule.ActuatorID).
				Msg("fast-path heuristic hit, bypassing LLM")
			e.pushEval("info", "fast-path heuristic hit", map[string]any{
				"rule_id":    matchedRule.ID,
				"event_type": ev.Type,
				"actuator":   matchedRule.ActuatorID,
			})
			return e.executeActuator(ctx, matchedRule.ActuatorID, ev)
		}
	} else {
		e.log.Debug().
			Str("type", ev.Type).
			Msg("user prompt/request event; bypassing fast-path heuristic cache")
	}

	// 2. Slow-Path LLM Decision Loop
	e.pushEval("debug", "heuristic miss; invoking LLM", map[string]any{"event_type": ev.Type})

	var manifests []sdk.ActuatorManifest
	if entries, err := os.ReadDir(e.binDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				actuatorID := entry.Name()
				m, err := e.getActuatorManifest(ctx, actuatorID)
				if err != nil {
					e.log.Warn().
						Err(err).
						Str("actuator", actuatorID).
						Msg("failed to inspect actuator manifest, using fallback")
					manifests = append(manifests, sdk.ActuatorManifest{
						ID:          actuatorID,
						Description: "Available actuator binary (failed to load detailed manifest description)",
					})
				} else {
					manifests = append(manifests, m)
				}
			}
		}
	}

	// Fetch historical state memory or metadata here (e.g. from core SQLite store)
	history := []string{"system bootstrap", "last execution succeeded"}
	if len(manifests) > 0 {
		history = append(
			history,
			"Available Actuators (you must use EXACTLY one of these IDs when using the 'invoke' action):",
		)
		for _, m := range manifests {
			capsJSON, _ := json.Marshal(m.Capabilities)
			history = append(
				history,
				fmt.Sprintf(
					"  - ID: %s\n    Description: %s\n    Capabilities: %s",
					m.ID,
					m.Description,
					string(capsJSON),
				),
			)
		}
	} else {
		history = append(history, "No actuators are currently compiled/available.")
	}

	decision, err := e.llm.AnalyzeEvent(ctx, ev, history)
	if err != nil {
		e.pushEval("error", "LLM analysis failed", map[string]any{"error": err.Error()})
		return errors.Wrap(err, "failed core LLM event analysis")
	}

	switch decision.Action {
	case "invoke":
		actuatorID := decision.ActuatorID
		// Try to normalize/fuzzy-match if the requested actuator ID doesn't exist
		if _, err := os.Stat(filepath.Join(e.binDir, actuatorID)); err != nil {
			normalized := strings.TrimSuffix(actuatorID, "-actuator")
			if _, err2 := os.Stat(filepath.Join(e.binDir, normalized)); err2 == nil {
				e.log.Info().
					Str("original", actuatorID).
					Str("normalized", normalized).
					Msg("normalized actuator ID from LLM decision")
				actuatorID = normalized
			}
		}

		e.log.Info().Str("actuator", actuatorID).Msg("LLM directed invocation of existing actuator")
		e.pushEval("info", "LLM: invoke actuator", map[string]any{
			"actuator":  actuatorID,
			"event_id":  ev.ID,
			"heuristic": decision.HeuristicTTL.String(),
		})

		// Cache the decision as a heuristic rule if TTL > 0
		if decision.HeuristicTTL > 0 && ev.Type != "clara.request" && ev.Type != "clara.prompt" {
			e.RegisterRule(HeuristicRule{
				ID:         fmt.Sprintf("rule-%s-%d", ev.Type, time.Now().UnixNano()),
				EventType:  ev.Type,
				ActuatorID: actuatorID,
				TTL:        decision.HeuristicTTL,
			})
		} else {
			e.log.Debug().
				Str("type", ev.Type).
				Msg("skipping fast-path heuristic caching for uncacheable event or TTL=0")
		}

		return e.executeActuator(ctx, actuatorID, ev)




	case "build":
		e.log.Warn().
			Str("actuator", decision.ActuatorID).
			Msg("LLM directed builder mode entry: compiling new logic")
		e.pushEval("warn", "LLM: builder mode", map[string]any{
			"actuator": decision.ActuatorID,
			"event_id": ev.ID,
		})

		if e.builder == nil {
			e.pushEval("error", "builder unavailable", map[string]any{
				"actuator": decision.ActuatorID,
				"reason":   "builder was not initialised (check workspace directory config)",
			})
			return errors.New("builder not configured: cannot compile new actuator")
		}

		// Run compiler loop
		maxRetries := 3
		currentCode := decision.ProposedCode
		var res CompileResult
		for attempt := 0; attempt < maxRetries; attempt++ {
			var err error
			res, err = e.builder.CompileAndVerify(ctx, decision.ActuatorID, currentCode)
			if err != nil {
				e.pushEval("error", "builder error", map[string]any{"error": err.Error()})
				return errors.Wrap(err, "builder execution failure")
			}
			if res.Success {
				break
			}

			// Log the failure to loghub
			e.log.Error().
				Str("diagnostics", res.CompilerError).
				Msgf("compilation failed (attempt %d/%d), routing diagnostics back to LLM", attempt+1, maxRetries)
			e.pushEval(
				"error",
				fmt.Sprintf("compile failed (attempt %d/%d)", attempt+1, maxRetries),
				map[string]any{
					"actuator": decision.ActuatorID,
					"error":    res.CompilerError,
				},
			)

			if attempt == maxRetries-1 {
				return errors.Newf(
					"compilation failed after %d attempts: %s",
					maxRetries,
					res.CompilerError,
				)
			}

			// Query LLM for refinement
			refinement, err := e.llm.RefineCode(ctx, res.CompilerError, currentCode)
			if err != nil {
				e.pushEval("error", "refinement failed", map[string]any{"error": err.Error()})
				return errors.Wrap(err, "LLM code refinement failed")
			}
			currentCode = refinement.ProposedCode
		}

		e.log.Info().Str("binary", res.BinaryPath).Msg("actuator successfully compiled and loaded")
		e.pushEval("info", "actuator compiled", map[string]any{
			"actuator": decision.ActuatorID,
			"binary":   res.BinaryPath,
		})

		// Copy binary to e.binDir so both the evaluator and subsequent lookups can access it.
		destPath := filepath.Join(e.binDir, decision.ActuatorID)
		if err := os.MkdirAll(e.binDir, 0o700); err == nil {
			if data, err := os.ReadFile(res.BinaryPath); err == nil {
				_ = os.WriteFile(destPath, data, 0o755)
			}
		}

		// Refresh manifest cache for the newly built actuator
		if m, err := e.inspectActuator(ctx, decision.ActuatorID); err == nil {
			e.mu.Lock()
			e.manifestCache[decision.ActuatorID] = m
			e.mu.Unlock()
		}

		// Register rule to fast-path
		e.RegisterHeuristic(ev.Type, decision.ActuatorID, 24*time.Hour)

		return e.executeActuator(ctx, decision.ActuatorID, ev)

	case "patch":
		e.log.Info().
			Str("actuator", decision.ActuatorID).
			Int("patches", len(decision.Patches)).
			Msg("LLM directed patch mode: applying search/replace edits")
		e.pushEval("info", "LLM: patch mode", map[string]any{
			"actuator": decision.ActuatorID,
			"patches":  len(decision.Patches),
			"event_id": ev.ID,
		})

		if e.builder == nil {
			e.pushEval("error", "builder unavailable", map[string]any{
				"actuator": decision.ActuatorID,
				"reason":   "builder was not initialised (check workspace directory config)",
			})
			return errors.New("builder not configured: cannot apply patches")
		}

		// Apply patches, then compile and verify the result, with the same
		// multi-turn self-healing loop used for full builds.
		maxRetries := 3
		currentPatches := decision.Patches
		var res CompileResult
		for attempt := 0; attempt < maxRetries; attempt++ {
			patchErr := e.builder.ApplyPatches(ctx, decision.ActuatorID, currentPatches)
			if patchErr != nil {
				e.pushEval("error", "patch application failed", map[string]any{
					"actuator": decision.ActuatorID,
					"error":    patchErr.Error(),
				})
				if attempt == maxRetries-1 {
					return errors.Wrap(patchErr, "patch application failed after max retries")
				}
				// Feed the patch error back to the LLM so it can emit a corrected patch.
				refinement, refErr := e.llm.RefineCode(ctx, patchErr.Error(), nil)
				if refErr != nil {
					return errors.Wrap(refErr, "LLM code refinement after patch failure")
				}
				currentPatches = refinement.Patches
				continue
			}

			// Patches applied; read the resulting files and compile.
			updatedCode, readErr := e.builder.ReadActuatorFiles(ctx, decision.ActuatorID)
			if readErr != nil {
				return errors.Wrap(readErr, "failed to read actuator files after patching")
			}

			var compErr error
			res, compErr = e.builder.CompileAndVerify(ctx, decision.ActuatorID, updatedCode)
			if compErr != nil {
				e.pushEval("error", "builder error after patch", map[string]any{"error": compErr.Error()})
				return errors.Wrap(compErr, "builder execution failure after patch")
			}
			if res.Success {
				break
			}

			e.log.Error().
				Str("diagnostics", res.CompilerError).
				Msgf("compile failed after patch (attempt %d/%d)", attempt+1, maxRetries)
			e.pushEval(
				"error",
				fmt.Sprintf("compile failed after patch (attempt %d/%d)", attempt+1, maxRetries),
				map[string]any{
					"actuator": decision.ActuatorID,
					"error":    res.CompilerError,
				},
			)

			if attempt == maxRetries-1 {
				return errors.Newf(
					"compilation failed after patching (%d attempts): %s",
					maxRetries,
					res.CompilerError,
				)
			}

			// Ask LLM to refine the patch based on the compiler error.
			refinement, refErr := e.llm.RefineCode(ctx, res.CompilerError, updatedCode)
			if refErr != nil {
				return errors.Wrap(refErr, "LLM code refinement after patch compile failure")
			}
			currentPatches = refinement.Patches
		}

		e.log.Info().Str("binary", res.BinaryPath).Msg("actuator successfully patched and compiled")
		e.pushEval("info", "actuator patched and compiled", map[string]any{
			"actuator": decision.ActuatorID,
			"binary":   res.BinaryPath,
		})

		// Copy binary to e.binDir.
		destPath := filepath.Join(e.binDir, decision.ActuatorID)
		if err := os.MkdirAll(e.binDir, 0o700); err == nil {
			if data, err := os.ReadFile(res.BinaryPath); err == nil {
				_ = os.WriteFile(destPath, data, 0o755)
			}
		}

		// Refresh manifest cache.
		if m, err := e.inspectActuator(ctx, decision.ActuatorID); err == nil {
			e.mu.Lock()
			e.manifestCache[decision.ActuatorID] = m
			e.mu.Unlock()
		}

		e.RegisterHeuristic(ev.Type, decision.ActuatorID, 24*time.Hour)

		return e.executeActuator(ctx, decision.ActuatorID, ev)

	case "ignore":
		e.log.Debug().Str("type", ev.Type).Msg("LLM evaluated event as ignorable noise")
		e.pushEval("debug", "LLM: ignore event", map[string]any{"event_type": ev.Type})
		return nil

	default:
		return errors.Newf("unsupported evaluator decision action: %q", decision.Action)
	}
}

// noopLLMClient is a placeholder LLM client that ignores all events until a real adapter is wired in.
type noopLLMClient struct{}

func (noopLLMClient) AnalyzeEvent(
	_ context.Context,
	_ CloudEvent,
	_ []string,
) (AnalysisResult, error) {
	return AnalysisResult{Action: "ignore"}, nil
}

func (noopLLMClient) RefineCode(
	_ context.Context,
	_ string,
	_ map[string]string,
) (AnalysisResult, error) {
	return AnalysisResult{Action: "ignore"}, nil
}

// NoopLLMClient returns an LLMClient that ignores all events.
// Use this as a placeholder until a real LLM adapter is available.
func NoopLLMClient() LLMClient { return noopLLMClient{} }

// pluginCmd returns an exec.Cmd for launching an actuator binary subprocess.
// The context is honoured: if ctx is cancelled the subprocess is killed.
func pluginCmd(ctx context.Context, binaryPath string) *exec.Cmd {
	return exec.CommandContext(ctx, binaryPath) //nolint:gosec // path validated by caller
}

// executeActuator starts the target actuator subprocess via gRPC plugin loaders
func (e *Evaluator) executeActuator(ctx context.Context, actuatorID string, ev CloudEvent) error {
	defer func() {
		if r := recover(); r != nil {
			e.log.Error().Interface("panic", r).Msg("panic during actuator execution; rolling back")
			if errRollback := e.rollbackActuator(ctx, actuatorID); errRollback != nil {
				e.log.Error().Err(errRollback).Msg("failed to rollback actuator after panic")
			}
		}
	}()

	binaryPath := filepath.Join(e.binDir, actuatorID)
	if _, err := os.Stat(binaryPath); err != nil {
		return errors.Wrapf(err, "actuator binary not found for %q at %s", actuatorID, binaryPath)
	}

	e.log.Info().
		Str("actuator", actuatorID).
		Str("binary", binaryPath).
		Msg("launching actuator subprocess")

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  sdk.HandshakeConfig,
		Plugins:          sdk.PluginMap,
		Cmd:              pluginCmd(ctx, binaryPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
	})
	defer client.Kill()

	rpcClient, err := client.Client()
	if err != nil {
		e.log.Error().
			Err(err).
			Str("actuator", actuatorID).
			Msg("failed to start actuator subprocess, rolling back")
		if errRollback := e.rollbackActuator(ctx, actuatorID); errRollback != nil {
			e.log.Error().Err(errRollback).Msg("failed to rollback actuator")
		}
		return errors.Wrap(err, "failed to connect to actuator subprocess")
	}

	raw, err := rpcClient.Dispense("actuator")
	if err != nil {
		return errors.Wrap(err, "failed to dispense actuator")
	}

	actuator, ok := raw.(sdk.Actuator)
	if !ok {
		return errors.New("dispensed plugin does not implement sdk.Actuator")
	}

	sdkEvent := sdk.Event{
		ID:     ev.ID,
		Source: ev.Source,
		Type:   ev.Type,
		Time:   ev.Time,
		Data:   ev.Data,
	}

	result, err := actuator.Execute(ctx, sdkEvent)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "connection is shut down") ||
			strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "broken pipe") ||
			strings.Contains(errStr, "connection refused") {
			e.log.Error().
				Err(err).
				Str("actuator", actuatorID).
				Msg("actuator connection lost during execution (crash), rolling back")
			if errRollback := e.rollbackActuator(ctx, actuatorID); errRollback != nil {
				e.log.Error().Err(errRollback).Msg("failed to rollback actuator")
			}
		}
		return errors.Wrapf(err, "actuator %q execution error", actuatorID)
	}

	if result.Success {
		e.log.Info().
			Str("actuator", actuatorID).
			Str("output", result.Output).
			Msg("actuator execution succeeded")
		if e.hub != nil {
			e.hub.PushActuator(actuatorID, "info", "execution succeeded", map[string]any{
				"output":   result.Output,
				"event_id": ev.ID,
			})
		}
	} else {
		e.log.Warn().
			Str("actuator", actuatorID).
			Str("output", result.Output).
			Msg("actuator execution reported failure")
		if e.hub != nil {
			e.hub.PushActuator(actuatorID, "warn", "execution reported failure", map[string]any{
				"output":   result.Output,
				"event_id": ev.ID,
			})
		}
	}

	if result.Retry {
		e.log.Info().
			Str("actuator", actuatorID).
			Dur("delay", result.Delay).
			Msg("actuator requested retry; re-queuing event")
		go func() {
			if result.Delay > 0 {
				timer := time.NewTimer(result.Delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			if err := e.executeActuator(ctx, actuatorID, ev); err != nil {
				e.log.Error().Err(err).Str("actuator", actuatorID).Msg("retry execution failed")
			}
		}()
	}

	return nil
}

// rollbackActuator retrieves the last stable compiled binary from the builder's git history
// and overwrites the active binary in e.binDir, pushing a warning to the log hub.
func (e *Evaluator) rollbackActuator(ctx context.Context, actuatorID string) error {
	if e.builder == nil {
		return errors.New("builder not configured; cannot rollback actuator")
	}

	binData, err := e.builder.GetLatestStableBinary(ctx, actuatorID)
	if err != nil {
		return errors.Wrapf(err, "failed to retrieve latest stable binary for %s", actuatorID)
	}

	destPath := filepath.Join(e.binDir, actuatorID)
	if err := os.WriteFile(destPath, binData, 0o755); err != nil {
		return errors.Wrapf(
			err,
			"failed to overwrite binary with rolled-back stable version for %s",
			actuatorID,
		)
	}

	e.pushEval("warn", "actuator rolled back to stable version", map[string]any{
		"actuator": actuatorID,
	})
	if e.hub != nil {
		e.hub.PushActuator(
			actuatorID,
			"warn",
			"rolled back to stable version due to execution crash or launch failure",
			nil,
		)
	}

	return nil
}

// inspectActuator runs the compiled actuator subprocess to query its manifest.
func (e *Evaluator) inspectActuator(
	ctx context.Context,
	actuatorID string,
) (sdk.ActuatorManifest, error) {
	binaryPath := filepath.Join(e.binDir, actuatorID)
	if _, err := os.Stat(binaryPath); err != nil {
		return sdk.ActuatorManifest{}, errors.Wrapf(
			err,
			"actuator binary not found for %q at %s",
			actuatorID,
			binaryPath,
		)
	}

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  sdk.HandshakeConfig,
		Plugins:          sdk.PluginMap,
		Cmd:              pluginCmd(ctx, binaryPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
	})
	defer client.Kill()

	rpcClient, err := client.Client()
	if err != nil {
		return sdk.ActuatorManifest{}, errors.Wrap(
			err,
			"failed to connect to actuator subprocess for inspection",
		)
	}

	raw, err := rpcClient.Dispense("actuator")
	if err != nil {
		return sdk.ActuatorManifest{}, errors.Wrap(
			err,
			"failed to dispense actuator for inspection",
		)
	}

	actuator, ok := raw.(sdk.Actuator)
	if !ok {
		return sdk.ActuatorManifest{}, errors.New(
			"dispensed plugin does not implement sdk.Actuator",
		)
	}

	return actuator.Manifest(), nil
}

// getActuatorManifest retrieves the manifest for the given actuatorID. It uses the cached copy if
// available, otherwise launches the binary to query it.
func (e *Evaluator) getActuatorManifest(
	ctx context.Context,
	actuatorID string,
) (sdk.ActuatorManifest, error) {
	e.mu.RLock()
	m, exists := e.manifestCache[actuatorID]
	e.mu.RUnlock()
	if exists {
		return m, nil
	}

	// Not cached, inspect the binary.
	manifest, err := e.inspectActuator(ctx, actuatorID)
	if err != nil {
		return sdk.ActuatorManifest{}, err
	}

	e.mu.Lock()
	e.manifestCache[actuatorID] = manifest
	e.mu.Unlock()

	return manifest, nil
}
