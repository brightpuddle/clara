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
// Maintains a long-lived SSE connection to the eve relay and forwards
// Discord events into Clara's event bus. Reconnects automatically on error.
func (d *Discord) StreamEvents() (<-chan contract.Event, error) {
	if d.cfg.EveURL == "" || d.cfg.Machine == "" {
		return nil, errors.New("discord: not configured, cannot stream events")
	}
	ch := make(chan contract.Event, 64)
	go d.sseLoop(ch)
	return ch, nil
}

func (d *Discord) sseLoop(ch chan<- contract.Event) {
	backoff := 2 * time.Second
	for {
		if err := d.sseConnect(ch); err != nil {
			log.Error().
				Err(err).
				Str("machine", d.cfg.Machine).
				Dur("backoff", backoff).
				Msg("discord SSE disconnected, reconnecting")
		}
		time.Sleep(backoff)
		if backoff < 60*time.Second {
			backoff *= 2
		}
	}
}

func (d *Discord) sseConnect(ch chan<- contract.Event) error {
	url := fmt.Sprintf("%s/api/discord/events?machine=%s", d.cfg.EveURL, d.cfg.Machine)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return errors.Wrap(err, "build SSE request")
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.Secret)
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
	log.Info().Str("machine", d.cfg.Machine).Msg("discord SSE stream connected")

	scanner := bufio.NewScanner(resp.Body)
	var eventType, dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ":") {
			continue // SSE comment
		}
		if line == "" {
			if eventType != "" && dataLine != "" {
				d.dispatchSSEEvent(ch, eventType, dataLine)
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

func (d *Discord) dispatchSSEEvent(ch chan<- contract.Event, eventType, rawData string) {
	// rawData is the JSON envelope: { "type": "...", "data": {...} }
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(rawData), &env); err != nil {
		log.Warn().Err(err).Str("raw", rawData).Msg("discord SSE: failed to parse event envelope")
		return
	}
	// EmitNotification(namespace, method, params) is called as ("discord", method, ...).
	// The supervisor matches triggers as namespace+"."+method == trigger name.
	// So ev.Name must be just the method part (e.g. "message_created"), not
	// the fully-qualified "discord.message_created".
	evName := env.Type
	if strings.HasPrefix(evName, "discord.") {
		evName = strings.TrimPrefix(evName, "discord.")
	}
	ev := contract.Event{
		Name: evName,
		Data: []byte(env.Data),
	}
	select {
	case ch <- ev:
	default:
		log.Warn().Str("type", env.Type).Msg("discord SSE: event channel full, dropping event")
	}
}
