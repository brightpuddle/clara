package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// MacOSIntegration is the interface for native macOS capabilities.
type MacOSIntegration interface {
	Integration
}

// --- RPC Wrappers ---

type MacOSIntegrationRPC struct {
	IntegrationRPC
}

type MacOSIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl MacOSIntegration
}

func (s *MacOSIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *MacOSIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *MacOSIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *MacOSIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type MacOSIntegrationPlugin struct {
	Impl MacOSIntegration
}

func (p *MacOSIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &MacOSIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *MacOSIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &MacOSIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
