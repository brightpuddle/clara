package chrome

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// commandTimeout is the maximum time to wait for the extension to respond to a
// single tool call.
const commandTimeout = 5 * time.Minute

// heartbeatInterval is how often to send a ping to the extension.
const heartbeatInterval = 15 * time.Second

// commandResult carries the raw JSON result or an error string from the extension.
type commandResult struct {
	Result json.RawMessage
	Error  string
}

// bridgeResponse is the JSON shape of messages arriving from the extension.
type bridgeResponse struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Version string          `json:"version"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
}

// Bridge manages the Unix Domain Socket connection from the Chrome extension
// and routes MCP tool calls through it. It is safe for concurrent use.
type Bridge struct {
	mu              sync.Mutex
	conn            net.Conn
	pending         map[string]chan commandResult
	log             zerolog.Logger
	currentVersion  string
	updateExtension func() error // called when extension is out of date
}

func newBridge(log zerolog.Logger, currentVersion string, updateFn func() error) *Bridge {
	return &Bridge{
		pending:         make(map[string]chan commandResult),
		log:             log,
		currentVersion:  currentVersion,
		updateExtension: updateFn,
	}
}

// IsConnected reports whether the Chrome extension is currently connected.
func (b *Bridge) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conn != nil
}

// execute sends a command to the extension and waits for its response.
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
			"Chrome extension not connected; please ensure Native Messaging is set up and Chrome has been restarted",
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

	cmd := map[string]any{
		"id":     id,
		"tool":   tool,
		"params": params,
	}

	raw, err := json.Marshal(cmd)
	if err != nil {
		return nil, errors.Wrap(err, "marshal command")
	}

	if _, err := conn.Write(append(raw, '\n')); err != nil {
		return nil, errors.Wrap(err, "send command to extension")
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

func (b *Bridge) handleConn(conn net.Conn) {
	b.mu.Lock()
	old := b.conn
	b.conn = conn
	b.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}

	b.log.Info().Msg("Chrome extension connected via UDS")

	defer func() {
		b.mu.Lock()
		if b.conn == conn {
			b.conn = nil
		}
		b.mu.Unlock()
		_ = conn.Close()
		b.log.Info().Msg("Chrome extension disconnected")
	}()

	decoder := json.NewDecoder(conn)
	for {
		var resp bridgeResponse
		if err := decoder.Decode(&resp); err != nil {
			break
		}
		b.dispatch(resp, conn)
	}
}

// dispatch routes an incoming message from the extension to the waiting caller.
func (b *Bridge) dispatch(resp bridgeResponse, conn net.Conn) {
	// Handle heartbeat/system messages.
	if resp.Type == "pong" {
		return
	}
	if resp.Type == "ping" {
		msg, _ := json.Marshal(map[string]any{"type": "pong"})
		_, _ = conn.Write(append(msg, '\n'))
		return
	}

	// Version handshake: extension sends {type:"hello", version:"..."} on every
	// connect. If it's out of date we write updated files to disk and ask it to
	// reload; the reconnect loop in the extension handles the rest.
	if resp.Type == "hello" {
		extVer := resp.Version
		b.log.Info().Str("ext_version", extVer).Str("server_version", b.currentVersion).
			Msg("Chrome extension connected")

		if extVer != b.currentVersion {
			b.log.Warn().
				Str("ext_version", extVer).
				Str("server_version", b.currentVersion).
				Msg("Extension version mismatch — updating extension files and requesting reload")

			if b.updateExtension != nil {
				if err := b.updateExtension(); err != nil {
					b.log.Error().Err(err).Msg("Failed to update extension files")
				}
			}

			msg, _ := json.Marshal(map[string]any{"type": "update"})
			_, _ = conn.Write(append(msg, '\n'))
		}
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

// Serve starts the UDS bridge server and blocks until ctx is cancelled.
func (b *Bridge) Serve(ctx context.Context) error {
	dataDir := filepath.Join(os.Getenv("HOME"), ".local", "share", "clara")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	udsPath := filepath.Join(dataDir, "chrome-bridge.sock")
	_ = os.Remove(udsPath)

	udsLn, err := net.Listen("unix", udsPath)
	if err != nil {
		return errors.Wrap(err, "listen unix")
	}
	defer udsLn.Close()

	b.log.Info().Str("uds", udsPath).Msg("Chrome bridge listening (Native Messaging)")

	// Close the listener when the context is done so Accept unblocks.
	go func() {
		<-ctx.Done()
		udsLn.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		for {
			c, err := udsLn.Accept()
			if err != nil {
				if ctx.Err() == nil {
					errCh <- errors.Wrap(err, "uds accept")
				}
				return
			}
			go b.handleConn(c)
		}
	}()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			b.mu.Lock()
			conn := b.conn
			b.mu.Unlock()
			if conn != nil {
				msg, _ := json.Marshal(map[string]any{"type": "ping"})
				if _, err := conn.Write(append(msg, '\n')); err != nil {
					// Ping write failed — the connection is dead. Close it so
					// handleConn's decoder loop also exits and we go back to
					// waiting for a fresh extension connection.
					b.log.Warn().Err(err).Msg("Heartbeat failed — closing stale connection")
					b.mu.Lock()
					if b.conn == conn {
						b.conn = nil
					}
					b.mu.Unlock()
					conn.Close()
				}
			}
		case err := <-errCh:
			return err
		}
	}
}
