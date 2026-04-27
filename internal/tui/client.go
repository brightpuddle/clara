package tui

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/toolcatalog"
)

type IPCClient struct {
	cfg *config.Config
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
