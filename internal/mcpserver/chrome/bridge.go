// Package chrome provides a built-in MCP server for Chrome browser automation.
// It bridges MCP tool calls to a companion Chrome extension over a local
// WebSocket connection. The extension connects to ws://localhost:<port> and
// executes browser actions (navigate, click, fill, upload, etc.) on demand.
package chrome

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// commandTimeout is the maximum time to wait for the extension to respond to a
// single tool call. Real browser automation against complex pages like Facebook
// can legitimately take a while, especially with intentional human delays.
const commandTimeout = 5 * time.Minute

// heartbeatInterval is how often to send a ping to the extension.
const heartbeatInterval = 15 * time.Second

// commandResult carries the raw JSON result or an error string from the extension.
type commandResult struct {
	Result json.RawMessage
	Error  string
}

// wsResponse is the JSON shape of messages arriving from the extension.
type wsResponse struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// Bridge manages the WebSocket connection from the Chrome extension and routes
// MCP tool calls through it. It is safe for concurrent use.
type Bridge struct {
	mu      sync.Mutex
	conn    *websocket.Conn
	pending map[string]chan commandResult
	log     zerolog.Logger
}

func newBridge(log zerolog.Logger) *Bridge {
	return &Bridge{
		pending: make(map[string]chan commandResult),
		log:     log,
	}
}

// IsConnected reports whether the Chrome extension is currently connected.
func (b *Bridge) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conn != nil
}

// execute sends a command to the extension and waits for its response. It
// returns an error if the extension is not connected, the context is cancelled,
// the call times out, or the extension reports an error.
func (b *Bridge) execute(
	ctx context.Context,
	tool string,
	params map[string]any,
) (json.RawMessage, error) {
	b.mu.Lock()
	conn := b.conn
	if conn == nil {
		b.mu.Unlock()
		return nil, errors.New(
			"Chrome extension not connected; load the clara extension in Chrome",
		)
	}

	id := uuid.New().String()
	ch := make(chan commandResult, 1)
	b.pending[id] = ch
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	msg, err := json.Marshal(map[string]any{
		"id":     id,
		"tool":   tool,
		"params": params,
	})
	if err != nil {
		return nil, errors.Wrap(err, "marshal command")
	}

	b.mu.Lock()
	writeErr := conn.WriteMessage(websocket.TextMessage, msg)
	b.mu.Unlock()
	if writeErr != nil {
		return nil, errors.Wrap(writeErr, "send command to extension")
	}

	select {
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "command cancelled")
	case <-time.After(commandTimeout):
		return nil, errors.Newf("command %q timed out after %s", tool, commandTimeout)
	case res := <-ch:
		if res.Error != "" {
			return nil, errors.New(res.Error)
		}
		return res.Result, nil
	}
}

// callTool is a convenience wrapper that passes all MCP tool arguments to the
// extension as-is and formats the response as an MCP text result.
func (b *Bridge) callTool(
	ctx context.Context,
	tool string,
	params map[string]any,
) (json.RawMessage, error) {
	return b.execute(ctx, tool, params)
}

// upgrader accepts WebSocket connections from any origin. The bridge only listens
// on localhost so origin validation provides no meaningful security boundary.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// handleWS is the HTTP handler for the WebSocket upgrade endpoint. Chrome only
// allows one native connection at a time, so a new connection replaces the old.
func (b *Bridge) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		b.log.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	b.mu.Lock()
	old := b.conn
	b.conn = conn
	b.mu.Unlock()

	if old != nil {
		old.Close()
	}

	b.log.Info().Str("remote", r.RemoteAddr).Msg("Chrome extension connected")

	defer func() {
		b.mu.Lock()
		if b.conn == conn {
			b.conn = nil
		}
		b.mu.Unlock()
		conn.Close()
		b.log.Info().Msg("Chrome extension disconnected")
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(
				err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
			) {
				b.log.Debug().Err(err).Msg("WebSocket read error")
			}
			break
		}
		b.dispatch(msg)
	}
}

// dispatch routes an incoming message from the extension to the waiting caller.
func (b *Bridge) dispatch(msg []byte) {
	var resp wsResponse
	if err := json.Unmarshal(msg, &resp); err != nil {
		b.log.Error().Err(err).Msg("Failed to parse message from extension")
		return
	}

	// Handle heartbeat/system messages
	if resp.Type == "pong" {
		b.log.Trace().Msg("Received pong from extension")
		return
	}
	if resp.Type == "ping" {
		b.log.Trace().Msg("Received ping from extension; sending pong")
		b.mu.Lock()
		if b.conn != nil {
			_ = b.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
		}
		b.mu.Unlock()
		return
	}

	if resp.ID == "" {
		return // unsolicited message — ignore
	}

	b.mu.Lock()
	ch, ok := b.pending[resp.ID]
	b.mu.Unlock()

	if !ok {
		return // already timed out or was a stale response
	}

	select {
	case ch <- commandResult{Result: resp.Result, Error: resp.Error}:
	default:
	}
}

// Serve starts the HTTP/WebSocket bridge server on addr (e.g. "localhost:48765")
// and blocks until ctx is cancelled.
func (b *Bridge) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", b.handleWS)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	b.log.Info().Str("addr", addr).Msg("Chrome bridge listening for extension")

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- errors.Wrap(err, "chrome bridge server")
		}
	}()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutCtx)
			return nil
		case <-ticker.C:
			b.mu.Lock()
			if b.conn != nil {
				err := b.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`))
				if err != nil {
					b.log.Debug().Err(err).Msg("Failed to send ping to extension")
				}
			}
			b.mu.Unlock()
		case err := <-errCh:
			return err
		}
	}
}
