package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// NoteInfo describes a note in the Zettelkasten vault.
type NoteInfo struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	Tags        []string       `json:"tags"`
	Wikilinks   []string       `json:"wikilinks"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
}

// NoteDetail includes the full content of a note.
type NoteDetail struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	Content     string         `json:"content"`
	Tags        []string       `json:"tags"`
	Wikilinks   []string       `json:"wikilinks"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
}

// TagEntry is a tag name and the number of notes that carry it.
type TagEntry struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// ZkIntegrationPlugin is a thin plugin.Plugin wrapper for the zk integration.
type ZkIntegrationPlugin struct{ Impl Integration }

func (p *ZkIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &IntegrationRPCServer{Impl: p.Impl}, nil
}

func (p *ZkIntegrationPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &IntegrationRPC{Client: c}, nil
}
