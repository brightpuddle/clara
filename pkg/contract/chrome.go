package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// ChromeIntegrationPlugin is a thin plugin.Plugin wrapper for the chrome integration.
type ChromeIntegrationPlugin struct{ Impl Integration }

func (p *ChromeIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *ChromeIntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
