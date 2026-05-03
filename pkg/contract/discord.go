package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// DiscordIntegrationPlugin is the go-plugin wrapper for the Discord integration.
type DiscordIntegrationPlugin struct{ Impl Integration }

func (p *DiscordIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *DiscordIntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
