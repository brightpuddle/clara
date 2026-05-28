package supervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hashicorp/go-plugin"
	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/pkg/sdk"
)

// LLMClient represents the interface to execute evaluator queries against the LLM kernel.
type LLMClient interface {
	AnalyzeEvent(ctx context.Context, ev CloudEvent, history []string) (AnalysisResult, error)
}

// AnalysisResult represents the decision of the LLM Evaluator.
type AnalysisResult struct {
	Action            string            `json:"action"`                       // "invoke", "build", "ignore"
	ActuatorID        string            `json:"actuator_id"`                  // ID of the actuator to run or compile
	ProposedCode      map[string]string `json:"proposed_code,omitempty"`      // Code to build on "build" action (filename -> content)
	HeuristicTTL      time.Duration     `json:"heuristic_ttl,omitempty"`      // How long to cache this routing decision in the fast-path
}

// Evaluator manages fast-path routing and triggers dynamic self-modification loops in Clara V2.
type Evaluator struct {
	log     zerolog.Logger
	bus     *EventBus
	llm     LLMClient
	builder *Builder
	binDir  string // directory containing actuator binaries

	mu         sync.RWMutex
	heuristics map[string]heuristicRoute // eventType -> actuatorID/route info
}

type heuristicRoute struct {
	actuatorID string
	expiresAt  time.Time
}

// NewEvaluator creates a new Evaluator.
// binDir is the directory where actuator binaries live (e.g. ~/.local/share/clara/bin).
func NewEvaluator(log zerolog.Logger, bus *EventBus, llm LLMClient, builder *Builder, binDir string) *Evaluator {
	if binDir == "" {
		home, _ := os.UserHomeDir()
		binDir = filepath.Join(home, ".local", "share", "clara", "bin")
	}
	return &Evaluator{
		log:        log.With().Str("component", "evaluator").Logger(),
		bus:        bus,
		llm:        llm,
		builder:    builder,
		binDir:     binDir,
		heuristics: make(map[string]heuristicRoute),
	}
}

// RegisterHeuristic manually registers a fast-path routing rule.
func (e *Evaluator) RegisterHeuristic(eventType string, actuatorID string, ttl time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.heuristics[eventType] = heuristicRoute{
		actuatorID: actuatorID,
		expiresAt:  time.Now().Add(ttl),
	}
	e.log.Debug().Str("type", eventType).Str("actuator", actuatorID).Msg("registered fast-path heuristic")
}

// OnEvent processes an incoming event. It prioritizes fast-path heuristics and falls back to LLM analysis.
func (e *Evaluator) OnEvent(ctx context.Context, ev CloudEvent) error {
	e.log.Info().Str("event_id", ev.ID).Str("type", ev.Type).Msg("processing ingress event")

	// 1. Fast-Path Heuristic Check
	e.mu.RLock()
	route, exists := e.heuristics[ev.Type]
	e.mu.RUnlock()

	if exists && time.Now().Before(route.expiresAt) {
		e.log.Info().Str("type", ev.Type).Str("actuator", route.actuatorID).Msg("fast-path heuristic hit, bypassing LLM")
		return e.executeActuator(ctx, route.actuatorID, ev)
	}

	// 2. Slow-Path LLM Decision Loop
	e.log.Debug().Str("type", ev.Type).Msg("heuristic cache miss/expired; invoking core LLM evaluator")
	
	// Fetch historical state memory or metadata here (e.g. from core SQLite store)
	history := []string{"system bootstrap", "last execution succeeded"}
	
	decision, err := e.llm.AnalyzeEvent(ctx, ev, history)
	if err != nil {
		return errors.Wrap(err, "failed core LLM event analysis")
	}

	switch decision.Action {
	case "invoke":
		e.log.Info().Str("actuator", decision.ActuatorID).Msg("LLM directed invocation of existing actuator")
		
		// Cache the decision as a heuristic for high throughput on subsequent events
		ttl := decision.HeuristicTTL
		if ttl <= 0 {
			ttl = 1 * time.Hour // Default cache TTL
		}
		e.RegisterHeuristic(ev.Type, decision.ActuatorID, ttl)
		
		return e.executeActuator(ctx, decision.ActuatorID, ev)

	case "build":
		e.log.Warn().Str("actuator", decision.ActuatorID).Msg("LLM directed builder mode entry: compiling new logic")
		
		// Run compiler loop
		res, err := e.builder.CompileAndVerify(ctx, decision.ActuatorID, decision.ProposedCode)
		if err != nil {
			return errors.Wrap(err, "builder execution failure")
		}

		if !res.Success {
			e.log.Error().Str("diagnostics", res.CompilerError).Msg("compilation failed, routing diagnostics back to LLM")
			return errors.Newf("compilation failed: %s", res.CompilerError)
		}

		e.log.Info().Str("binary", res.BinaryPath).Msg("actuator successfully compiled and loaded")
		
		// Register rule to fast-path
		e.RegisterHeuristic(ev.Type, decision.ActuatorID, 24*time.Hour)
		
		return e.executeActuator(ctx, decision.ActuatorID, ev)

	case "ignore":
		e.log.Debug().Str("type", ev.Type).Msg("LLM evaluated event as ignorable noise")
		return nil

	default:
		return errors.Newf("unsupported evaluator decision action: %q", decision.Action)
	}
}

// noopLLMClient is a placeholder LLM client that ignores all events until a real adapter is wired in.
type noopLLMClient struct{}

func (noopLLMClient) AnalyzeEvent(_ context.Context, _ CloudEvent, _ []string) (AnalysisResult, error) {
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
	binaryPath := filepath.Join(e.binDir, actuatorID)
	if _, err := os.Stat(binaryPath); err != nil {
		return errors.Wrapf(err, "actuator binary not found for %q at %s", actuatorID, binaryPath)
	}

	e.log.Info().
		Str("actuator", actuatorID).
		Str("binary", binaryPath).
		Msg("launching actuator subprocess")

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: sdk.HandshakeConfig,
		Plugins:         sdk.PluginMap,
		Cmd:             pluginCmd(ctx, binaryPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
	})
	defer client.Kill()

	rpcClient, err := client.Client()
	if err != nil {
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
		return errors.Wrapf(err, "actuator %q execution error", actuatorID)
	}

	if result.Success {
		e.log.Info().
			Str("actuator", actuatorID).
			Str("output", result.Output).
			Msg("actuator execution succeeded")
	} else {
		e.log.Warn().
			Str("actuator", actuatorID).
			Str("output", result.Output).
			Msg("actuator execution reported failure")
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
