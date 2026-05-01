package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// TmuxSession describes a running tmux session.
type TmuxSession struct {
	Name      string `json:"name"`
	Windows   int    `json:"windows"`
	CreatedAt int64  `json:"created_at"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Attached  bool   `json:"attached"`
}

// TmuxIntegrationPlugin is a thin plugin.Plugin wrapper for the tmux integration.
type TmuxIntegrationPlugin struct{ Impl Integration }

func (p *TmuxIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *TmuxIntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
