package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/agents"
	"github.com/brightpuddle/clara/internal/cockpit"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/supervisor"
)

// startAttentionNotifier subscribes to the event bus and triggers desktop notifications (ClaraBridge)
// when an agent requests attention or transitions to a blocked/terminal state.
func startAttentionNotifier(
	ctx context.Context,
	bus *supervisor.EventBus,
	reg *registry.Registry,
	cfg cockpit.AttentionConfig,
	log zerolog.Logger,
) {
	if !cfg.Enabled {
		return
	}

	notifier := cockpit.NewAttentionNotifier(cfg)
	sub, unsub := bus.SubscribeCloud()

	go func() {
		defer unsub()
		for {
			select {
			case <-ctx.Done():
				return
			case ce, ok := <-sub:
				if !ok {
					return
				}
				if !strings.HasPrefix(ce.Type, "agent.") {
					continue
				}

				eventType := strings.TrimPrefix(ce.Type, "agent.")
				dataMap := ce.Data
				if dataMap == nil {
					continue
				}

				sessionID, _ := dataMap["session"].(string)
				host, _ := dataMap["host"].(string)
				rawPayload, _ := dataMap["payload"]

				payloadBytes, err := json.Marshal(rawPayload)
				if err != nil {
					continue
				}

				var shouldNotify bool
				var title, body string

				switch agents.EventType(eventType) {
				case agents.TypeAttentionRequested:
					var ar agents.AttentionRequested
					if err := json.Unmarshal(payloadBytes, &ar); err == nil {
						shouldNotify, title, body = notifier.EvaluateAttention(ar, sessionID, host, "")
					}

				case agents.TypeSessionStatus:
					var st agents.SessionStatus
					if err := json.Unmarshal(payloadBytes, &st); err == nil {
						shouldNotify, title, body = notifier.EvaluateStatus(st, sessionID, host, "")
					}
				}

				if shouldNotify && title != "" {
					log.Info().Str("session", sessionID).Str("host", host).Str("title", title).Msg("emitting desktop notification")
					_, callErr := reg.Call(ctx, "macos.notify_send", map[string]any{
						"title": title,
						"body":  body,
					})
					if callErr != nil {
						// Fall back to notify.send
						_, _ = reg.Call(ctx, "notify.send", map[string]any{
							"message": fmt.Sprintf("%s\n%s", title, body),
						})
					}
				}
			}
		}
	}()
}
