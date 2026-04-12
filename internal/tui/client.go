package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/toolcatalog"
	"github.com/mark3labs/mcp-go/server"
)

type IPCClient struct {
	cfg *config.Config
}

type DynamicRegistration struct {
	Name       string `json:"name"`
	Token      string `json:"token"`
	SocketPath string `json:"socket_path"`
}

type StatusCounts struct {
	Servers int `json:"servers"`
	Tools   int `json:"tools"`
	Intents int `json:"intents"`
}

type ToolInfo = toolcatalog.Tool
type ProviderSummary = toolcatalog.Provider
type IntentSummary struct {
	ID string `json:"id"`
}

func NewIPCClient(cfg *config.Config) *IPCClient {
	return &IPCClient{cfg: cfg}
}

func (c *IPCClient) IsRunning() bool {
	conn, err := net.DialTimeout("unix", c.cfg.ControlSocketPath(), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (c *IPCClient) Request(method string, params map[string]any) (*ipc.Response, error) {
	return c.Do(ipc.Request{Method: method, Params: params})
}

func (c *IPCClient) Do(req ipc.Request) (*ipc.Response, error) {
	resp, err := c.DoRaw(req)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return resp, fmt.Errorf("agent error: %s", resp.Error)
	}
	return resp, nil
}

func (c *IPCClient) DoRaw(req ipc.Request) (*ipc.Response, error) {
	conn, err := net.DialTimeout("unix", c.cfg.ControlSocketPath(), 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// Use a long deadline for blocking calls (e.g. CLI interactive prompts)
	_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp ipc.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

func (c *IPCClient) StatusCounts() (StatusCounts, error) {
	resp, err := c.Do(ipc.Request{Method: ipc.MethodStatus})
	if err != nil {
		return StatusCounts{}, err
	}
	var counts StatusCounts
	if err := decodeInto(resp.Data, &counts); err != nil {
		return StatusCounts{}, err
	}
	return counts, nil
}

func decodeInto(data any, dst any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	return nil
}

// StartDynamicMCP registers the TUI as a dynamic MCP peer with the daemon
// and serves the local notification tools over a reverse connection.
func (c *IPCClient) StartDynamicMCP(ctx context.Context, mcpSrv *server.MCPServer) error {
	resp, err := c.Do(
		ipc.Request{Method: ipc.MethodMCPRegister, Params: map[string]any{"name": "tui"}},
	)
	if err != nil {
		return fmt.Errorf("register dynamic mcp: %w", err)
	}
	var reg DynamicRegistration
	if err := decodeInto(resp.Data, &reg); err != nil {
		return fmt.Errorf("decode registration: %w", err)
	}

	var conn net.Conn
	var dialErr error
	for i := 0; i < 5; i++ {
		conn, dialErr = net.DialTimeout("unix", reg.SocketPath, 2*time.Second)
		if dialErr == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if dialErr != nil {
		return fmt.Errorf("dial dynamic attach socket after retries: %w", dialErr)
	}

	if err := json.NewEncoder(conn).Encode(map[string]string{"token": reg.Token}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("send token: %w", err)
	}

	var handshake struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&handshake); err != nil {
		_ = conn.Close()
		return fmt.Errorf("read handshake: %w", err)
	}
	if handshake.Error != "" {
		_ = conn.Close()
		return fmt.Errorf("handshake error: %s", handshake.Error)
	}

	s := server.NewStdioServer(mcpSrv)
	return s.Listen(ctx, conn, conn)
}

func (c *IPCClient) LoadTUIHistory(limit int) ([]map[string]any, error) {
	resp, err := c.Do(ipc.Request{
		Method: ipc.MethodTUIHistory,
		Params: map[string]any{"limit": float64(limit)},
	})
	if err != nil {
		return nil, err
	}
	var history []map[string]any
	if err := decodeInto(resp.Data, &history); err != nil {
		return nil, err
	}
	return history, nil
}

func (c *IPCClient) ClearTUIHistory() error {
	_, err := c.Do(ipc.Request{Method: ipc.MethodTUIClear})
	return err
}

func (c *IPCClient) UpdateTUIAnswer(id int64, intentID, answer string, resume bool) error {
	// 1. Update the record in the DB
	_, err := c.Do(ipc.Request{
		Method: ipc.MethodTUIAnswer,
		Params: map[string]any{
			"id":     float64(id),
			"answer": answer,
		},
	})
	if err != nil {
		return err
	}

	// 2. Trigger resume if requested and there is an intentID
	if resume && intentID != "" {
		_, err = c.Do(ipc.Request{
			Method: ipc.MethodStart,
			Params: map[string]any{
				"id":    intentID,
				"input": answer,
			},
		})
		return err
	}
	return nil
}
