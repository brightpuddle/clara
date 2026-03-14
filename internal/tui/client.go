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
)

type IPCClient struct {
	cfg *config.Config
}

type StatusCounts struct {
	Servers int `json:"servers"`
	Tools   int `json:"tools"`
	Intents int `json:"intents"`
}

type ProviderSummary = toolcatalog.Provider
type ToolParam = toolcatalog.Param
type ToolInfo = toolcatalog.Tool

type IntentSummary struct {
	ID string `json:"id"`
}

func NewIPCClient(cfg *config.Config) *IPCClient {
	return &IPCClient{cfg: cfg}
}

func (c *IPCClient) ControlSocketPath() string {
	return c.cfg.ControlSocketPath()
}

func (c *IPCClient) DynamicSocketPath() string {
	return c.cfg.DynamicMCPSocketPath()
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
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp ipc.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

func (c *IPCClient) RegisterDynamicMCP(
	ctx context.Context,
	name string,
) (DynamicRegistration, error) {
	resp, err := c.Do(
		ipc.Request{Method: ipc.MethodMCPRegister, Params: map[string]any{"name": name}},
	)
	if err != nil {
		return DynamicRegistration{}, err
	}
	var reg DynamicRegistration
	if err := decodeInto(resp.Data, &reg); err != nil {
		return DynamicRegistration{}, err
	}
	return reg, nil
}

func (c *IPCClient) UnregisterDynamicMCP(ctx context.Context, name string) error {
	_, err := c.Do(
		ipc.Request{Method: ipc.MethodMCPUnregister, Params: map[string]any{"name": name}},
	)
	return err
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

func (c *IPCClient) ListTools(filter string) ([]ToolInfo, error) {
	params := map[string]any{"view": "tools"}
	if filter != "" {
		params["filter"] = filter
	}
	resp, err := c.Do(ipc.Request{Method: ipc.MethodToolList, Params: params})
	if err != nil {
		return nil, err
	}
	var tools []ToolInfo
	if err := decodeInto(resp.Data, &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func (c *IPCClient) ShowTool(name string) (ToolInfo, error) {
	resp, err := c.Do(ipc.Request{
		Method: ipc.MethodToolShow,
		Params: map[string]any{"name": name},
	})
	if err != nil {
		return ToolInfo{}, err
	}
	var tool ToolInfo
	if err := decodeInto(resp.Data, &tool); err != nil {
		return ToolInfo{}, err
	}
	return tool, nil
}

func (c *IPCClient) ListProviders() ([]ProviderSummary, error) {
	resp, err := c.Do(ipc.Request{Method: ipc.MethodToolList})
	if err != nil {
		return nil, err
	}
	var providers []ProviderSummary
	if err := decodeInto(resp.Data, &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

func (c *IPCClient) ListIntents() ([]IntentSummary, error) {
	resp, err := c.Do(ipc.Request{Method: ipc.MethodList})
	if err != nil {
		return nil, err
	}
	var intents []IntentSummary
	if err := decodeInto(resp.Data, &intents); err != nil {
		return nil, err
	}
	return intents, nil
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
