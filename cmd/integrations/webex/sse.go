package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog/log"
)

// StreamEvents implements contract.EventStreamer.
// Opens a long-lived SSE connection to the eve relay and forwards Webex
// webhook events into Clara's event bus. Reconnects automatically on error.
func (w *Webex) StreamEvents() (<-chan contract.Event, error) {
	if w.cfg.EveURL == "" || w.cfg.Machine == "" {
		return nil, errors.New("webex: not configured, cannot stream events")
	}
	ch := make(chan contract.Event, 64)
	go w.sseLoop(ch)
	return ch, nil
}

func (w *Webex) sseLoop(ch chan<- contract.Event) {
	backoff := 2 * time.Second
	for {
		if err := w.sseConnect(ch); err != nil {
			log.Error().
				Err(err).
				Str("machine", w.cfg.Machine).
				Dur("backoff", backoff).
				Msg("webex SSE disconnected, reconnecting")
		}
		time.Sleep(backoff)
		if backoff < 60*time.Second {
			backoff *= 2
		}
	}
}

func (w *Webex) sseConnect(ch chan<- contract.Event) error {
	url := fmt.Sprintf("%s/api/webex/events?machine=%s", w.cfg.EveURL, w.cfg.Machine)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return errors.Wrap(err, "build SSE request")
	}
	req.Header.Set("Authorization", "Bearer "+w.cfg.Secret)
	req.Header.Set("Accept", "text/event-stream")

	// No timeout — connection is intentionally long-lived.
	sseClient := &http.Client{}
	resp, err := sseClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "SSE connect")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.Newf("SSE endpoint returned status %d", resp.StatusCode)
	}
	log.Info().Str("machine", w.cfg.Machine).Msg("webex SSE stream connected")

	scanner := bufio.NewScanner(resp.Body)
	var eventType, dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ":") {
			continue // SSE comment / keepalive
		}
		if line == "" {
			if eventType != "" && dataLine != "" {
				w.dispatchSSEEvent(ch, eventType, dataLine)
			}
			eventType, dataLine = "", ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	return errors.Wrap(scanner.Err(), "SSE scanner")
}

func (w *Webex) dispatchSSEEvent(ch chan<- contract.Event, eventType, rawData string) {
	// rawData is the JSON envelope: { "type": "...", "data": {...} }
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(rawData), &env); err != nil {
		log.Warn().Err(err).Str("raw", rawData).Msg("webex SSE: failed to parse event envelope")
		return
	}
	// ev.Name must be just the method part (e.g. "message_created") so the
	// supervisor can match triggers as namespace + "." + method.
	evName := env.Type
	if strings.HasPrefix(evName, "webex.") {
		evName = strings.TrimPrefix(evName, "webex.")
	}
	ev := contract.Event{
		Name: evName,
		Data: []byte(env.Data),
	}
	select {
	case ch <- ev:
	default:
		log.Warn().Str("type", env.Type).Msg("webex SSE: event channel full, dropping event")
	}
}
