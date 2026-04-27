package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// ZkIntegration is the interface for Zettelkasten vault integrations.
type ZkIntegration interface {
	Integration
	ListNotes() ([]NoteInfo, error)
	GetNote(note string) (NoteDetail, error)
	CreateNote(name, content string) (NoteDetail, error)
	UpdateNote(note, content string) error
	DeleteNote(note string) error
	ResolveWikilink(target string) (string, error)
	ListTags() ([]TagEntry, error)
	GetNotesByTag(tag string) ([]NoteInfo, error)
	SearchNotes(query string, limit int) ([]NoteInfo, error)
	ReloadVault() error
}

type NoteInfo struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	Tags        []string       `json:"tags"`
	Wikilinks   []string       `json:"wikilinks"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
}

type NoteDetail struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	Content     string         `json:"content"`
	Tags        []string       `json:"tags"`
	Wikilinks   []string       `json:"wikilinks"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
}

type TagEntry struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// --- RPC Wrappers ---

type ZkIntegrationRPC struct {
	IntegrationRPC
}

func (g *ZkIntegrationRPC) ListNotes() ([]NoteInfo, error) {
	var resp []NoteInfo
	err := g.Client.Call("Plugin.ListNotes", EmptyArgs{}, &resp)
	return resp, err
}

func (g *ZkIntegrationRPC) GetNote(note string) (NoteDetail, error) {
	var resp NoteDetail
	err := g.Client.Call("Plugin.GetNote", note, &resp)
	return resp, err
}

func (g *ZkIntegrationRPC) CreateNote(name, content string) (NoteDetail, error) {
	var resp NoteDetail
	err := g.Client.Call("Plugin.CreateNote", CreateNoteArgs{Name: name, Content: content}, &resp)
	return resp, err
}

func (g *ZkIntegrationRPC) UpdateNote(note, content string) error {
	return g.Client.Call("Plugin.UpdateNote", UpdateNoteArgs{Note: note, Content: content}, &struct{}{})
}

func (g *ZkIntegrationRPC) DeleteNote(note string) error {
	return g.Client.Call("Plugin.DeleteNote", note, &struct{}{})
}

func (g *ZkIntegrationRPC) ResolveWikilink(target string) (string, error) {
	var resp string
	err := g.Client.Call("Plugin.ResolveWikilink", target, &resp)
	return resp, err
}

func (g *ZkIntegrationRPC) ListTags() ([]TagEntry, error) {
	var resp []TagEntry
	err := g.Client.Call("Plugin.ListTags", EmptyArgs{}, &resp)
	return resp, err
}

func (g *ZkIntegrationRPC) GetNotesByTag(tag string) ([]NoteInfo, error) {
	var resp []NoteInfo
	err := g.Client.Call("Plugin.GetNotesByTag", tag, &resp)
	return resp, err
}

func (g *ZkIntegrationRPC) SearchNotes(query string, limit int) ([]NoteInfo, error) {
	var resp []NoteInfo
	err := g.Client.Call("Plugin.SearchNotes", SearchNotesArgs{Query: query, Limit: limit}, &resp)
	return resp, err
}

func (g *ZkIntegrationRPC) ReloadVault() error {
	return g.Client.Call("Plugin.ReloadVault", EmptyArgs{}, &struct{}{})
}

type CreateNoteArgs struct {
	Name    string
	Content string
}

type UpdateNoteArgs struct {
	Note    string
	Content string
}

type SearchNotesArgs struct {
	Query string
	Limit int
}

type ZkIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl ZkIntegration
}

func (s *ZkIntegrationRPCServer) ListNotes(args EmptyArgs, resp *[]NoteInfo) error {
	var err error
	*resp, err = s.Impl.ListNotes()
	return err
}

func (s *ZkIntegrationRPCServer) GetNote(note string, resp *NoteDetail) error {
	var err error
	*resp, err = s.Impl.GetNote(note)
	return err
}

func (s *ZkIntegrationRPCServer) CreateNote(args CreateNoteArgs, resp *NoteDetail) error {
	var err error
	*resp, err = s.Impl.CreateNote(args.Name, args.Content)
	return err
}

func (s *ZkIntegrationRPCServer) UpdateNote(args UpdateNoteArgs, resp *struct{}) error {
	return s.Impl.UpdateNote(args.Note, args.Content)
}

func (s *ZkIntegrationRPCServer) DeleteNote(note string, resp *struct{}) error {
	return s.Impl.DeleteNote(note)
}

func (s *ZkIntegrationRPCServer) ResolveWikilink(target string, resp *string) error {
	var err error
	*resp, err = s.Impl.ResolveWikilink(target)
	return err
}

func (s *ZkIntegrationRPCServer) ListTags(args EmptyArgs, resp *[]TagEntry) error {
	var err error
	*resp, err = s.Impl.ListTags()
	return err
}

func (s *ZkIntegrationRPCServer) GetNotesByTag(tag string, resp *[]NoteInfo) error {
	var err error
	*resp, err = s.Impl.GetNotesByTag(tag)
	return err
}

func (s *ZkIntegrationRPCServer) SearchNotes(args SearchNotesArgs, resp *[]NoteInfo) error {
	var err error
	*resp, err = s.Impl.SearchNotes(args.Query, args.Limit)
	return err
}

func (s *ZkIntegrationRPCServer) ReloadVault(args EmptyArgs, resp *struct{}) error {
	return s.Impl.ReloadVault()
}

func (s *ZkIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *ZkIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *ZkIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *ZkIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type ZkIntegrationPlugin struct {
	Impl ZkIntegration
}

func (p *ZkIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &ZkIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *ZkIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &ZkIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
