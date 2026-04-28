package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// FSIntegration is the interface for filesystem integrations.
type FSIntegration interface {
	Integration
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, content []byte) error
}

// --- RPC Wrappers ---

type FSIntegrationRPC struct {
	IntegrationRPC
}

func (g *FSIntegrationRPC) ReadFile(path string) ([]byte, error) {
	var resp []byte
	err := g.Client.Call("Plugin.ReadFile", path, &resp)
	return resp, err
}

func (g *FSIntegrationRPC) WriteFile(path string, content []byte) error {
	return g.Client.Call("Plugin.WriteFile", struct {
		Path    string
		Content []byte
	}{Path: path, Content: content}, &struct{}{})
}

type FSIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl FSIntegration
}

func (s *FSIntegrationRPCServer) ReadFile(path string, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.ReadFile(path)
	return err
}

func (s *FSIntegrationRPCServer) WriteFile(args struct {
	Path    string
	Content []byte
}, resp *struct{}) error {
	return s.Impl.WriteFile(args.Path, args.Content)
}

func (s *FSIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *FSIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *FSIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *FSIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type FSIntegrationPlugin struct {
	Impl FSIntegration
}

func (p *FSIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &FSIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *FSIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &FSIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
