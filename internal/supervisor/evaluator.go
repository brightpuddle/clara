package supervisor

import (
	"context"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
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
	log       zerolog.Logger
	bus       *EventBus
	llm       LLMClient
	builder   *Builder
	
	mu         sync.RWMutex
	heuristics map[string]heuristicRoute // eventType -> actuatorID/route info
}

type heuristicRoute struct {
	actuatorID string
	expiresAt  time.Time
}

// NewEvaluator creates a new Evaluator.
func NewEvaluator(log zerolog.Logger, bus *EventBus, llm LLMClient, builder *Builder) *Evaluator {
	return &Evaluator{
		log:        log.With().Str("component", "evaluator").Logger(),
		bus:        bus,
		llm:        llm,
		builder:    builder,
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

// executeActuator starts the target actuator subprocess via gRPC plugin loaders
func (e *Evaluator) executeActuator(ctx context.Context, actuatorID string, ev CloudEvent) error {
	e.log.Debug().Str("actuator", actuatorID).Msg("dispatching to actuator execution engine")
	// In Clara V2, this dynamically starts the compiled binary as a hashicorp/go-plugin gRPC subprocess
	return nil
}
