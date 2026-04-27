package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// ShellIntegration is the interface for shell-like integrations.
type ShellIntegration interface {
	Integration
	Run(command string) (string, error)
}

// --- RPC Wrappers ---

type ShellIntegrationRPC struct {
	IntegrationRPC
}

func (g *ShellIntegrationRPC) Run(command string) (string, error) {
	var resp string
	err := g.Client.Call("Plugin.Run", command, &resp)
	return resp, err
}

type ShellIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl ShellIntegration
}

func (s *ShellIntegrationRPCServer) Run(command string, resp *string) error {
	var err error
	*resp, err = s.Impl.Run(command)
	return err
}

// Standard methods need to be overridden to call s.Impl instead of s.IntegrationRPCServer.Impl
// because s.Impl is more specific (ShellIntegration) and covers both.

func (s *ShellIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *ShellIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *ShellIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *ShellIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type ShellIntegrationPlugin struct {
	Impl ShellIntegration
}

func (p *ShellIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &ShellIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *ShellIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &ShellIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
