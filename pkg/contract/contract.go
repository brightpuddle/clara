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

// --- Interfaces ---

type Integration interface {
	Configure(config []byte) error
	Tools() ([]byte, error)
	CallTool(name string, args []byte) ([]byte, error)
}

type ShellIntegration interface {
	Integration
	Run(command string) (string, error)
}

type Intent interface {
	Execute(name string, shell ShellIntegration) error
}

// --- ShellIntegration RPC Boilerplate ---

type CallToolArgs struct {
	Name string
	Args []byte
}

type ShellIntegrationRPC struct{ client *rpc.Client }

func (g *ShellIntegrationRPC) Configure(config []byte) error {
	var resp error
	err := g.client.Call("Plugin.Configure", config, &resp)
	if err != nil {
		return err
	}
	return resp
}

func (g *ShellIntegrationRPC) Tools() ([]byte, error) {
	var resp []byte
	err := g.client.Call("Plugin.Tools", new(interface{}), &resp)
	return resp, err
}

func (g *ShellIntegrationRPC) CallTool(name string, args []byte) ([]byte, error) {
	var resp []byte
	err := g.client.Call("Plugin.CallTool", CallToolArgs{Name: name, Args: args}, &resp)
	return resp, err
}

func (g *ShellIntegrationRPC) Run(command string) (string, error) {
	var resp string
	err := g.client.Call("Plugin.Run", command, &resp)
	return resp, err
}

type ShellIntegrationRPCServer struct {
	Impl ShellIntegration
}

func (s *ShellIntegrationRPCServer) Configure(config []byte, resp *error) error {
	*resp = s.Impl.Configure(config)
	return nil
}

func (s *ShellIntegrationRPCServer) Tools(args interface{}, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *ShellIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

func (s *ShellIntegrationRPCServer) Run(command string, resp *string) error {
	var err error
	*resp, err = s.Impl.Run(command)
	return err
}

type ShellIntegrationPlugin struct {
	Impl ShellIntegration
}

func (p *ShellIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &ShellIntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *ShellIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &ShellIntegrationRPC{client: c}, nil
}

// --- Intent RPC Boilerplate ---

type ExecuteArgs struct {
	Name    string
	ShellID uint32
}

type IntentRPC struct {
	client *rpc.Client
	broker *plugin.MuxBroker
}

func (g *IntentRPC) Execute(name string, shell ShellIntegration) error {
	// Register the shell implementation as a server so the plugin can call back
	shellID := g.broker.NextId()
	go g.broker.AcceptAndServe(shellID, &ShellIntegrationRPCServer{Impl: shell})

	return g.client.Call("Plugin.Execute", ExecuteArgs{
		Name:    name,
		ShellID: shellID,
	}, &struct{}{})
}

type IntentRPCServer struct {
	Impl   Intent
	broker *plugin.MuxBroker
}

func (s *IntentRPCServer) Execute(args ExecuteArgs, resp *struct{}) error {
	// Dial the host to get the shell client
	conn, err := s.broker.Dial(args.ShellID)
	if err != nil {
		return err
	}
	defer conn.Close()

	shell := &ShellIntegrationRPC{client: rpc.NewClient(conn)}
	return s.Impl.Execute(args.Name, shell)
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

// --- FSIntegration RPC Boilerplate ---

type FSIntegration interface {
	Integration
}

type FSIntegrationRPC struct{ client *rpc.Client }

func (g *FSIntegrationRPC) Configure(config []byte) error {
	var resp error
	err := g.client.Call("Plugin.Configure", config, &resp)
	if err != nil {
		return err
	}
	return resp
}

func (g *FSIntegrationRPC) Tools() ([]byte, error) {
	var resp []byte
	err := g.client.Call("Plugin.Tools", new(interface{}), &resp)
	return resp, err
}

func (g *FSIntegrationRPC) CallTool(name string, args []byte) ([]byte, error) {
	var resp []byte
	err := g.client.Call("Plugin.CallTool", CallToolArgs{Name: name, Args: args}, &resp)
	return resp, err
}

type FSIntegrationRPCServer struct {
	Impl FSIntegration
}

func (s *FSIntegrationRPCServer) Configure(config []byte, resp *error) error {
	*resp = s.Impl.Configure(config)
	return nil
}

func (s *FSIntegrationRPCServer) Tools(args interface{}, resp *[]byte) error {
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
	return &FSIntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *FSIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &FSIntegrationRPC{client: c}, nil
}
