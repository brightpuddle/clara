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

// Integration is the base interface for all Clara integrations.
type Integration interface {
	Configure(config []byte) error
	Description() (string, error)
	Tools() ([]byte, error)
	CallTool(name string, args []byte) ([]byte, error)
}

// Event is a real-time notification from a plugin.
type Event struct {
	Name string
	Data []byte // JSON encoded parameters
}

// EventStreamer is an optional interface for plugins that push real-time events.
type EventStreamer interface {
	StreamEvents() (<-chan Event, error)
}

// HTTPIntegration is an optional interface for plugins that handle HTTP requests.
type HTTPIntegration interface {
	HandleHTTP(method, path string, headers map[string]string, body []byte) (status int, resp []byte, err error)
}

// EmptyArgs is a placeholder for RPC calls that take no arguments.
type EmptyArgs struct{}

// CallToolArgs carries the tool name and its JSON-encoded arguments over RPC.
type CallToolArgs struct {
	Name string
	Args []byte
}

// HTTPRequest carries an HTTP request over RPC.
type HTTPRequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    []byte
}

// HTTPResponse carries an HTTP response over RPC.
type HTTPResponse struct {
	Status int
	Body   []byte
}

// IntegrationRPC implements the Integration interface over RPC.
// It can be embedded in more specific integration RPC clients.
type IntegrationRPC struct {
	Client *rpc.Client
}

func (g *IntegrationRPC) Configure(config []byte) error {
	return g.Client.Call("Plugin.Configure", config, &struct{}{})
}

func (g *IntegrationRPC) Description() (string, error) {
	var resp string
	err := g.Client.Call("Plugin.Description", EmptyArgs{}, &resp)
	return resp, err
}

func (g *IntegrationRPC) Tools() ([]byte, error) {
	var resp []byte
	err := g.Client.Call("Plugin.Tools", EmptyArgs{}, &resp)
	return resp, err
}

func (g *IntegrationRPC) CallTool(name string, args []byte) ([]byte, error) {
	var resp []byte
	err := g.Client.Call("Plugin.CallTool", CallToolArgs{Name: name, Args: args}, &resp)
	return resp, err
}

func (g *IntegrationRPC) HandleHTTP(method, path string, headers map[string]string, body []byte) (int, []byte, error) {
	req := HTTPRequest{Method: method, Path: path, Headers: headers, Body: body}
	var resp HTTPResponse
	err := g.Client.Call("Plugin.HandleHTTP", req, &resp)
	return resp.Status, resp.Body, err
}

// IntegrationRPCServer handles base Integration RPC calls.
// It can be embedded in more specific integration RPC servers.
type IntegrationRPCServer struct {
	Impl Integration
}

func (s *IntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *IntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *IntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *IntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

func (s *IntegrationRPCServer) HandleHTTP(req HTTPRequest, resp *HTTPResponse) error {
	if httpImpl, ok := s.Impl.(HTTPIntegration); ok {
		status, body, err := httpImpl.HandleHTTP(req.Method, req.Path, req.Headers, req.Body)
		resp.Status = status
		resp.Body = body
		return err
	}
	resp.Status = 404
	resp.Body = []byte("Not Implemented")
	return nil
}

// IntegrationPlugin is a generic plugin.Plugin wrapper for any Integration.
// Use this (or a named variant below) in go-plugin ServeConfig.Plugins maps.
type IntegrationPlugin struct{ Impl Integration }

func (p *IntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *IntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
