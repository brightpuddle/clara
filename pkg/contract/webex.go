package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// WebexIntegrationPlugin is the go-plugin wrapper for the Webex integration.
type WebexIntegrationPlugin struct{ Impl Integration }

func (p *WebexIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *WebexIntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
