package webexapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// IngestManager coordinates real-time event ingestion from Webex.
// It tries to connect via WebSockets first, and if that fails, falls back to polling.
type IngestManager struct {
	client     *Client
	router     *Router
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	deviceURL  string
	mu         sync.Mutex
	lastPoll   time.Time
	seenMsgs   map[string]bool
	seenMsgsMu sync.RWMutex
}

// NewIngestManager creates a new IngestManager.
func NewIngestManager(client *Client, router *Router) *IngestManager {
	return &IngestManager{
		client:   client,
		router:   router,
		seenMsgs: make(map[string]bool),
		lastPoll: time.Now().Add(-1 * time.Hour), // Check past hour on startup
	}
}

// Start launches the ingestion worker.
func (im *IngestManager) Start(ctx context.Context) {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.ctx, im.cancel = context.WithCancel(ctx)
	im.wg.Add(1)

	go func() {
		defer im.wg.Done()
		im.run()
	}()
}

// Stop stops the ingestion worker and cleans up any device registration.
func (im *IngestManager) Stop() {
	im.mu.Lock()
	if im.cancel != nil {
		im.cancel()
	}
	im.mu.Unlock()

	im.wg.Wait()
	im.unregisterDevice()
}

func (im *IngestManager) run() {
	wsErr := im.runWebSocket()
	if wsErr != nil {
		log.Warn().Err(wsErr).Msg("webex: websocket ingestion failed, falling back to polling")
		im.runPolling()
	}
}

// runWebSocket attempts to register a device and connect via Mercury WebSockets.
func (im *IngestManager) runWebSocket() error {
	token, err := im.client.getToken()
	if err != nil {
		return fmt.Errorf("get auth token: %w", err)
	}

	// 1. Register device
	regURL := "https://wdm-a.wbx2.com/wdm/api/v1/devices"
	regBody, _ := json.Marshal(map[string]string{
		"deviceName":     "clara-local-bot",
		"deviceType":     "DESKTOP",
		"localizedModel": "go-client",
		"model":          "go-client",
		"name":           "clara-local-bot",
		"systemName":     "clara",
		"systemVersion":  "1.0.0",
	})

	req, err := http.NewRequestWithContext(im.ctx, http.MethodPost, regURL, bytes.NewReader(regBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("device registration request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("device registration status %d: %s", resp.StatusCode, body)
	}

	var regResp struct {
		WebSocketURL string `json:"webSocketUrl"`
		URL          string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return fmt.Errorf("decode device registration: %w", err)
	}

	im.mu.Lock()
	im.deviceURL = regResp.URL
	im.mu.Unlock()

	if regResp.WebSocketURL == "" {
		return fmt.Errorf("no webSocketUrl returned in registration")
	}

	// 2. Connect WebSocket
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)

	conn, _, err := dialer.DialContext(im.ctx, regResp.WebSocketURL, headers)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	defer conn.Close()

	log.Info().Msg("webex: connected to Mercury WebSockets successfully")

	// 3. Keep-alive and read loop
	errCh := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			im.handleRawWSMessage(msg)
		}
	}()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-im.ctx.Done():
			// Clean close
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil
		case err := <-errCh:
			return fmt.Errorf("websocket read error: %w", err)
		case <-ticker.C:
			// Send WebSocket ping
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return fmt.Errorf("websocket ping failed: %w", err)
			}
		}
	}
}

func (im *IngestManager) unregisterDevice() {
	im.mu.Lock()
	devURL := im.deviceURL
	im.deviceURL = ""
	im.mu.Unlock()

	if devURL == "" {
		return
	}

	token, err := im.client.getToken()
	if err != nil {
		return
	}

	req, err := http.NewRequest(http.MethodDelete, devURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
		log.Info().Msg("webex: unregistered device successfully")
	}
}

type mercuryActivity struct {
	Verb   string `json:"verb"`
	Actor  struct {
		EmailAddress string `json:"emailAddress"`
		ID           string `json:"id"`
	} `json:"actor"`
	Target struct {
		ID string `json:"id"`
	} `json:"target"`
	Object struct {
		ID string `json:"id"`
	} `json:"object"`
	Published string `json:"published"`
}

type mercuryEnvelope struct {
	Data struct {
		Activity mercuryActivity `json:"activity"`
	} `json:"data"`
}

func (im *IngestManager) handleRawWSMessage(msg []byte) {
	var env mercuryEnvelope
	if err := json.Unmarshal(msg, &env); err != nil {
		return
	}

	act := env.Data.Activity
	if act.Verb == "post" && act.Object.ID != "" {
		// New message posted. Fetch the message text using client.GetMessage to mirror webhook payload
		messageID := act.Object.ID
		
		im.seenMsgsMu.Lock()
		alreadySeen := im.seenMsgs[messageID]
		im.seenMsgs[messageID] = true
		im.seenMsgsMu.Unlock()

		if alreadySeen {
			return
		}

		go func() {
			msgDetails, err := im.client.GetMessage(messageID)
			if err != nil {
				log.Warn().Err(err).Str("message_id", messageID).Msg("webex: could not fetch message details for websocket event")
				return
			}
			
			evData, _ := json.Marshal(map[string]string{
				"message_id":   msgDetails.ID,
				"room_id":      msgDetails.RoomID,
				"room_type":    msgDetails.RoomType,
				"person_email": msgDetails.PersonEmail,
				"person_id":    msgDetails.PersonID,
				"text":         msgDetails.Text,
				"created":      msgDetails.Created,
			})
			im.router.Publish(Event{Type: "message_created", Data: evData})
		}()
	}
}

// runPolling executes an optimized polling loop.
func (im *IngestManager) runPolling() {
	log.Info().Msg("webex: starting polling fallback worker")
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial poll
	im.pollOnce()

	for {
		select {
		case <-im.ctx.Done():
			return
		case <-ticker.C:
			im.pollOnce()
		}
	}
}

func (im *IngestManager) pollOnce() {
	rooms, err := im.client.ListRooms(50)
	if err != nil {
		log.Warn().Err(err).Msg("webex poll: failed to list rooms")
		return
	}

	pollStart := time.Now()

	for _, room := range rooms {
		if room.LastActivity == "" {
			continue
		}

		laTime, err := time.Parse(time.RFC3339Nano, room.LastActivity)
		if err != nil {
			laTime, err = time.Parse(time.RFC3339, room.LastActivity)
		}

		if err == nil && laTime.Before(im.lastPoll) {
			// No new activity since last poll
			continue
		}

		// Fetch latest messages for this active room
		msgs, err := im.client.ListMessages(room.ID, 10)
		if err != nil {
			log.Warn().Err(err).Str("room_id", room.ID).Msg("webex poll: failed to list messages")
			continue
		}

		// Process messages in chronological order (ListMessages returns newest first, so we walk backwards)
		for i := len(msgs) - 1; i >= 0; i-- {
			msg := msgs[i]

			im.seenMsgsMu.Lock()
			seen := im.seenMsgs[msg.ID]
			im.seenMsgs[msg.ID] = true
			im.seenMsgsMu.Unlock()

			if seen {
				continue
			}

			// Parse message creation time
			mTime, err := time.Parse(time.RFC3339Nano, msg.Created)
			if err != nil {
				mTime, err = time.Parse(time.RFC3339, msg.Created)
			}
			if err == nil && mTime.Before(im.lastPoll) {
				// Old message from before we started tracking
				continue
			}

			evData, _ := json.Marshal(map[string]string{
				"message_id":   msg.ID,
				"room_id":      msg.RoomID,
				"room_type":    msg.RoomType,
				"person_email": msg.PersonEmail,
				"person_id":    msg.PersonID,
				"text":         msg.Text,
				"created":      msg.Created,
			})
			im.router.Publish(Event{Type: "message_created", Data: evData})
		}
	}

	im.lastPoll = pollStart
}
