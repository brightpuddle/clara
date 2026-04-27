package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// ChromeIntegration is the interface for Chrome browser automation.
type ChromeIntegration interface {
	Integration
	// We can add strongly typed methods here as needed.
}

// --- RPC Wrappers ---

type ChromeIntegrationRPC struct {
	IntegrationRPC
}

type ChromeIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl ChromeIntegration
}

func (s *ChromeIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *ChromeIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *ChromeIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *ChromeIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type ChromeIntegrationPlugin struct {
	Impl ChromeIntegration
}

func (p *ChromeIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &ChromeIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *ChromeIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &ChromeIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
