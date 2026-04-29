package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// TmuxSession describes a running tmux session.
type TmuxSession struct {
	Name      string `json:"name"`
	Windows   int    `json:"windows"`
	CreatedAt int64  `json:"created_at"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Attached  bool   `json:"attached"`
}

// TmuxIntegration is the interface for tmux session management.
type TmuxIntegration interface {
	Integration
	ListSessions() ([]TmuxSession, error)
	CreateSession(name, command string) error
	CapturePane(name string, limit int) (string, error)
	KillSession(name string) error
}

// --- RPC Wrappers ---

type TmuxCreateSessionArgs struct {
	Name    string
	Command string
}

type TmuxCapturePaneArgs struct {
	Name  string
	Limit int
}

type TmuxIntegrationRPC struct {
	IntegrationRPC
}

func (g *TmuxIntegrationRPC) ListSessions() ([]TmuxSession, error) {
	var resp []TmuxSession
	err := g.Client.Call("Plugin.ListSessions", EmptyArgs{}, &resp)
	return resp, err
}

func (g *TmuxIntegrationRPC) CreateSession(name, command string) error {
	return g.Client.Call("Plugin.CreateSession", TmuxCreateSessionArgs{
		Name:    name,
		Command: command,
	}, &struct{}{})
}

func (g *TmuxIntegrationRPC) CapturePane(name string, limit int) (string, error) {
	var resp string
	err := g.Client.Call("Plugin.CapturePane", TmuxCapturePaneArgs{
		Name:  name,
		Limit: limit,
	}, &resp)
	return resp, err
}

func (g *TmuxIntegrationRPC) KillSession(name string) error {
	return g.Client.Call("Plugin.KillSession", name, &struct{}{})
}

type TmuxIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl TmuxIntegration
}

func (s *TmuxIntegrationRPCServer) ListSessions(args EmptyArgs, resp *[]TmuxSession) error {
	var err error
	*resp, err = s.Impl.ListSessions()
	return err
}

func (s *TmuxIntegrationRPCServer) CreateSession(args TmuxCreateSessionArgs, resp *struct{}) error {
	return s.Impl.CreateSession(args.Name, args.Command)
}

func (s *TmuxIntegrationRPCServer) CapturePane(args TmuxCapturePaneArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.CapturePane(args.Name, args.Limit)
	return err
}

func (s *TmuxIntegrationRPCServer) KillSession(name string, resp *struct{}) error {
	return s.Impl.KillSession(name)
}

// Override base methods to route through s.Impl (which satisfies both Integration
// and TmuxIntegration) rather than the embedded IntegrationRPCServer.Impl.

func (s *TmuxIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *TmuxIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *TmuxIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *TmuxIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type TmuxIntegrationPlugin struct {
	Impl TmuxIntegration
}

func (p *TmuxIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &TmuxIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *TmuxIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &TmuxIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
