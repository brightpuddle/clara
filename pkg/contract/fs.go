package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// FSIntegration is the interface for filesystem integrations.
type FSIntegration interface {
	Integration
}

// --- RPC Wrappers ---

type FSIntegrationRPC struct {
	IntegrationRPC
}

type FSIntegrationRPCServer struct {
	IntegrationRPCServer
}

type FSIntegrationPlugin struct {
	Impl FSIntegration
}

func (p *FSIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &FSIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
	}, nil
}

func (p *FSIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &FSIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
