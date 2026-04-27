package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// HandshakeConfig is used to just do a basic check between the host and the plugin
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "CLARA_PLUGIN_MAGIC_COOKIE",
	MagicCookieValue: "hello_clara",
}

// --- Base Interfaces ---

// Integration is the base interface for all Clara integrations.
type Integration interface {
	Configure(config []byte) error
	Description() (string, error)
	Tools() ([]byte, error)
	CallTool(name string, args []byte) ([]byte, error)
}

// Context provides access to host-side services and integrations for intents.
type Context interface {
	Shell() (ShellIntegration, error)
	FS() (FSIntegration, error)
}

// Intent is the interface for native Go intents.
type Intent interface {
	Execute(name string, ctx Context) error
}

// --- Base RPC Helpers ---

type CallToolArgs struct {
	Name string
	Args []byte
}

// IntegrationRPC implements the Integration interface over RPC.
// It can be embedded in more specific integration RPC clients.
type IntegrationRPC struct {
	Client *rpc.Client
}

func (g *IntegrationRPC) Configure(config []byte) error {
	var resp error
	err := g.Client.Call("Plugin.Configure", config, &resp)
	if err != nil {
		return err
	}
	return resp
}

func (g *IntegrationRPC) Description() (string, error) {
	var resp string
	err := g.Client.Call("Plugin.Description", struct{}{}, &resp)
	return resp, err
}

func (g *IntegrationRPC) Tools() ([]byte, error) {
	var resp []byte
	err := g.Client.Call("Plugin.Tools", struct{}{}, &resp)
	return resp, err
}

func (g *IntegrationRPC) CallTool(name string, args []byte) ([]byte, error) {
	var resp []byte
	err := g.Client.Call("Plugin.CallTool", CallToolArgs{Name: name, Args: args}, &resp)
	return resp, err
}

// IntegrationRPCServer handles base Integration RPC calls.
// It can be embedded in more specific integration RPC servers.
type IntegrationRPCServer struct {
	Impl Integration
}

func (s *IntegrationRPCServer) Configure(config []byte, resp *error) error {
	*resp = s.Impl.Configure(config)
	return nil
}

func (s *IntegrationRPCServer) Description(args struct{}, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *IntegrationRPCServer) Tools(args struct{}, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *IntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

// --- Context RPC Boilerplate ---

type ContextRPC struct {
	client *rpc.Client
	broker *plugin.MuxBroker
}

func (g *ContextRPC) Shell() (ShellIntegration, error) {
	var id uint32
	err := g.client.Call("Plugin.Shell", new(interface{}), &id)
	if err != nil {
		return nil, err
	}
	conn, err := g.broker.Dial(id)
	if err != nil {
		return nil, err
	}
	return &ShellIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: rpc.NewClient(conn)}}, nil
}

func (g *ContextRPC) FS() (FSIntegration, error) {
	var id uint32
	err := g.client.Call("Plugin.FS", new(interface{}), &id)
	if err != nil {
		return nil, err
	}
	conn, err := g.broker.Dial(id)
	if err != nil {
		return nil, err
	}
	return &FSIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: rpc.NewClient(conn)}}, nil
}

type ContextRPCServer struct {
	Impl   Context
	broker *plugin.MuxBroker
}

func (s *ContextRPCServer) Shell(args interface{}, resp *uint32) error {
	impl, err := s.Impl.Shell()
	if err != nil {
		return err
	}
	*resp = s.broker.NextId()
	go s.broker.AcceptAndServe(*resp, &ShellIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: impl},
		Impl:                 impl,
	})
	return nil
}

func (s *ContextRPCServer) FS(args interface{}, resp *uint32) error {
	impl, err := s.Impl.FS()
	if err != nil {
		return err
	}
	*resp = s.broker.NextId()
	go s.broker.AcceptAndServe(*resp, &FSIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: impl},
	})
	return nil
}

// --- Intent RPC Boilerplate ---

type ExecuteArgs struct {
	Name      string
	ContextID uint32
}

type IntentRPC struct {
	client *rpc.Client
	broker *plugin.MuxBroker
}

func (g *IntentRPC) Execute(name string, ctx Context) error {
	// Register the context implementation as a server so the plugin can call back
	ctxID := g.broker.NextId()
	go g.broker.AcceptAndServe(ctxID, &ContextRPCServer{Impl: ctx, broker: g.broker})

	return g.client.Call("Plugin.Execute", ExecuteArgs{
		Name:      name,
		ContextID: ctxID,
	}, &struct{}{})
}

type IntentRPCServer struct {
	Impl   Intent
	broker *plugin.MuxBroker
}

func (s *IntentRPCServer) Execute(args ExecuteArgs, resp *struct{}) error {
	// Dial the host to get the context client
	conn, err := s.broker.Dial(args.ContextID)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := &ContextRPC{client: rpc.NewClient(conn), broker: s.broker}
	return s.Impl.Execute(args.Name, ctx)
}

type IntentPlugin struct {
	Impl Intent
}

func (p *IntentPlugin) Server(b *plugin.MuxBroker) (interface{}, error) {
	return &IntentRPCServer{Impl: p.Impl, broker: b}, nil
}

func (p *IntentPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntentRPC{client: c, broker: b}, nil
}
