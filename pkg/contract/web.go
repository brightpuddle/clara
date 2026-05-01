package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// SearchResult is a single result returned by a web search.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebIntegrationPlugin is a thin plugin.Plugin wrapper for the web integration.
type WebIntegrationPlugin struct{ Impl Integration }

func (p *WebIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *WebIntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
