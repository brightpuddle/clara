package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// WebIntegration is the interface for web search and scraping.
type WebIntegration interface {
	Integration
	Search(query string, limit int) ([]SearchResult, error)
	Read(url string) (string, error)
}

type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// --- RPC Wrappers ---

type WebSearchArgs struct {
	Query string
	Limit int
}

type WebIntegrationRPC struct {
	IntegrationRPC
}

func (g *WebIntegrationRPC) Search(query string, limit int) ([]SearchResult, error) {
	var resp []SearchResult
	err := g.Client.Call("Plugin.Search", WebSearchArgs{Query: query, Limit: limit}, &resp)
	return resp, err
}

func (g *WebIntegrationRPC) Read(url string) (string, error) {
	var resp string
	err := g.Client.Call("Plugin.Read", url, &resp)
	return resp, err
}

type WebIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl WebIntegration
}

func (s *WebIntegrationRPCServer) Search(args WebSearchArgs, resp *[]SearchResult) error {
	var err error
	*resp, err = s.Impl.Search(args.Query, args.Limit)
	return err
}

func (s *WebIntegrationRPCServer) Read(url string, resp *string) error {
	var err error
	*resp, err = s.Impl.Read(url)
	return err
}

func (s *WebIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *WebIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *WebIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *WebIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type WebIntegrationPlugin struct {
	Impl WebIntegration
}

func (p *WebIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &WebIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *WebIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &WebIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
