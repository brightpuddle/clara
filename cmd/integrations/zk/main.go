package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

const Description = "Built-in Zettelkasten integration: CRUD notes, resolve wikilinks, query by tag, and parse YAML frontmatter from Obsidian-style Markdown vaults."

type ZkPlugin struct {
	vault *Vault
	log   zerolog.Logger
}

func (p *ZkPlugin) Configure(config []byte) error {
	var cfg struct {
		VaultRoot string `json:"vault_root"`
		IndexPath string `json:"index_path"`
	}
	if len(config) > 0 {
		if err := json.Unmarshal(config, &cfg); err != nil {
			return err
		}
	}

	if cfg.VaultRoot == "" {
		return fmt.Errorf("vault_root is required")
	}

	p.log = zerolog.New(os.Stderr).With().Timestamp().Logger()
	v, err := Open(cfg.VaultRoot, cfg.IndexPath, p.log)
	if err != nil {
		return err
	}
	p.vault = v
	return nil
}

func (p *ZkPlugin) Description() (string, error) {
	return Description, nil
}

func (p *ZkPlugin) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool("note_list",
			mcp.WithDescription(
				"List all notes in the vault. Returns name, path, tags, wikilinks, and frontmatter for each note.",
			),
		),
		mcp.NewTool("note_get",
			mcp.WithDescription(
				"Get the full content and parsed metadata of a note by name or path.",
			),
			mcp.WithString("note",
				mcp.Required(),
				mcp.Description(
					"Note name (filename stem, case-insensitive) or absolute path to the .md file.",
				),
			),
		),
		mcp.NewTool("note_create",
			mcp.WithDescription(
				"Create a new note in the vault. Fails if a note with that name already exists.",
			),
			mcp.WithString("name",
				mcp.Required(),
				mcp.Description(
					"Filename stem for the new note (without .md extension). "+
						"A relative subdirectory may be included, e.g. \"projects/my-idea\".",
				),
			),
			mcp.WithString(
				"content",
				mcp.Required(),
				mcp.Description(
					"Full Markdown content for the note, including optional YAML frontmatter.",
				),
			),
		),
		mcp.NewTool("note_update",
			mcp.WithDescription(
				"Overwrite the content of an existing note. The note index is refreshed automatically.",
			),
			mcp.WithString("note",
				mcp.Required(),
				mcp.Description("Note name (case-insensitive) or absolute path to the .md file."),
			),
			mcp.WithString("content",
				mcp.Required(),
				mcp.Description("New Markdown content for the note."),
			),
		),
		mcp.NewTool("note_delete",
			mcp.WithDescription("Permanently delete a note from the vault."),
			mcp.WithString("note",
				mcp.Required(),
				mcp.Description("Note name (case-insensitive) or absolute path to the .md file."),
			),
		),
		mcp.NewTool("note_resolve_wikilink",
			mcp.WithDescription(
				"Resolve a [[wikilink]] target to the absolute path of the note it references. "+
					"Returns an empty string if no matching note is found in the vault.",
			),
			mcp.WithString("target",
				mcp.Required(),
				mcp.Description(
					"Wikilink target text, e.g. \"My Note\" from [[My Note]]. "+
						"Fragment identifiers (#section) are stripped before lookup.",
				),
			),
		),
		mcp.NewTool("tag_list",
			mcp.WithDescription(
				"List all tags present in the vault, with the count of notes that carry each tag.",
			),
		),
		mcp.NewTool("tag_notes",
			mcp.WithDescription(
				"Return all notes that carry a given tag. "+
					"The leading # is optional. Tag matching is case-insensitive.",
			),
			mcp.WithString("tag",
				mcp.Required(),
				mcp.Description("Tag to search for, e.g. \"project\" or \"#project\"."),
			),
		),
		mcp.NewTool("note_search",
			mcp.WithDescription(
				"Search for notes whose content contains a specific keyword (case-insensitive).",
			),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Keyword or phrase to search for."),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of results to return."),
			),
		),
		mcp.NewTool("vault_reload",
			mcp.WithDescription(
				"Rebuild the vault index from disk. "+
					"Call this if you have made changes to the vault outside of this server.",
			),
		),
	}
	return json.Marshal(tools)
}

func (p *ZkPlugin) CallTool(name string, args []byte) ([]byte, error) {
	if p.vault == nil {
		return nil, fmt.Errorf("ZkPlugin not configured")
	}

	var parsedArgs map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &parsedArgs); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
	}

	if p.vault.IsIndexing() {
		return nil, fmt.Errorf(
			"The Zettelkasten vault is currently being indexed. Please try again in a few seconds.",
		)
	}

	switch name {
	case "note_list":
		notes, err := p.ListNotes()
		if err != nil {
			return nil, err
		}
		return json.Marshal(notes)
	case "note_get":
		noteRef, _ := parsedArgs["note"].(string)
		note, err := p.GetNote(noteRef)
		if err != nil {
			return nil, err
		}
		return json.Marshal(note)
	case "note_create":
		noteName, _ := parsedArgs["name"].(string)
		content, _ := parsedArgs["content"].(string)
		note, err := p.CreateNote(noteName, content)
		if err != nil {
			return nil, err
		}
		return json.Marshal(note)
	case "note_update":
		noteRef, _ := parsedArgs["note"].(string)
		content, _ := parsedArgs["content"].(string)
		err := p.UpdateNote(noteRef, content)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"status": "success", "message": "updated"})
	case "note_delete":
		noteRef, _ := parsedArgs["note"].(string)
		err := p.DeleteNote(noteRef)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"status": "success", "message": "deleted"})
	case "note_resolve_wikilink":
		target, _ := parsedArgs["target"].(string)
		path, err := p.ResolveWikilink(target)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"target": target,
			"path":   path,
			"found":  path != "",
		})
	case "tag_list":
		tags, err := p.ListTags()
		if err != nil {
			return nil, err
		}
		return json.Marshal(tags)
	case "tag_notes":
		tag, _ := parsedArgs["tag"].(string)
		notes, err := p.GetNotesByTag(tag)
		if err != nil {
			return nil, err
		}
		return json.Marshal(notes)
	case "note_search":
		query, _ := parsedArgs["query"].(string)
		limitVal, _ := parsedArgs["limit"].(float64)
		notes, err := p.SearchNotes(query, int(limitVal))
		if err != nil {
			return nil, err
		}
		return json.Marshal(notes)
	case "vault_reload":
		err := p.ReloadVault()
		if err != nil {
			return nil, err
		}
		return json.Marshal(
			map[string]string{
				"status":  "success",
				"message": "Vault indexing started in background.",
			},
		)
	default:
		return nil, fmt.Errorf("tool %q not found", name)
	}
}

// --- ZkIntegration Implementation ---

func (p *ZkPlugin) ListNotes() ([]contract.NoteInfo, error) {
	notes := p.vault.AllNotes()
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].Path < notes[j].Path
	})
	infos := make([]contract.NoteInfo, len(notes))
	for i, n := range notes {
		infos[i] = noteToContractInfo(n)
	}
	return infos, nil
}

func (p *ZkPlugin) GetNote(noteRef string) (contract.NoteDetail, error) {
	n, err := p.resolveNote(noteRef)
	if err != nil {
		return contract.NoteDetail{}, err
	}

	content, err := os.ReadFile(n.Path)
	if err != nil {
		return contract.NoteDetail{}, fmt.Errorf("read file: %v", err)
	}

	return contract.NoteDetail{
		Name:        n.Name,
		Path:        n.Path,
		Content:     string(content),
		Tags:        n.Tags,
		Wikilinks:   n.Wikilinks,
		Frontmatter: n.Frontmatter,
	}, nil
}

func (p *ZkPlugin) CreateNote(name, content string) (contract.NoteDetail, error) {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	relPath := stem + ".md"
	absPath := filepath.Join(p.vault.Root(), relPath)

	if _, existing := p.vault.NoteByPath(absPath); existing {
		return contract.NoteDetail{}, fmt.Errorf("note %q already exists", stem)
	}
	if _, statErr := os.Stat(absPath); statErr == nil {
		return contract.NoteDetail{}, fmt.Errorf("file already exists at %q", absPath)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return contract.NoteDetail{}, fmt.Errorf("mkdir: %v", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return contract.NoteDetail{}, fmt.Errorf("write: %v", err)
	}
	if err := p.vault.IndexPath(context.Background(), absPath); err != nil {
		return contract.NoteDetail{}, fmt.Errorf("index: %v", err)
	}

	n, _ := p.vault.NoteByPath(absPath)
	return contract.NoteDetail{
		Name:        n.Name,
		Path:        n.Path,
		Content:     content,
		Tags:        n.Tags,
		Wikilinks:   n.Wikilinks,
		Frontmatter: n.Frontmatter,
	}, nil
}

func (p *ZkPlugin) UpdateNote(noteRef, content string) error {
	n, err := p.resolveNote(noteRef)
	if err != nil {
		return err
	}

	if writeErr := os.WriteFile(n.Path, []byte(content), 0o644); writeErr != nil {
		return fmt.Errorf("write: %v", writeErr)
	}
	if indexErr := p.vault.IndexPath(context.Background(), n.Path); indexErr != nil {
		return fmt.Errorf("index: %v", indexErr)
	}
	return nil
}

func (p *ZkPlugin) DeleteNote(noteRef string) error {
	n, err := p.resolveNote(noteRef)
	if err != nil {
		return err
	}

	if removeErr := os.Remove(n.Path); removeErr != nil {
		return fmt.Errorf("remove: %v", removeErr)
	}
	p.vault.RemovePath(context.Background(), n.Path)
	return nil
}

func (p *ZkPlugin) ResolveWikilink(target string) (string, error) {
	return p.vault.ResolveWikilink(target), nil
}

func (p *ZkPlugin) ListTags() ([]contract.TagEntry, error) {
	notes := p.vault.AllNotes()
	counts := make(map[string]int)
	for _, n := range notes {
		for _, t := range n.Tags {
			counts[t]++
		}
	}

	entries := make([]contract.TagEntry, 0, len(counts))
	for tag, count := range counts {
		entries = append(entries, contract.TagEntry{Tag: tag, Count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Tag < entries[j].Tag
	})
	return entries, nil
}

func (p *ZkPlugin) GetNotesByTag(tag string) ([]contract.NoteInfo, error) {
	notes := p.vault.NotesByTag(tag)
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].Path < notes[j].Path
	})
	infos := make([]contract.NoteInfo, len(notes))
	for i, n := range notes {
		infos[i] = noteToContractInfo(n)
	}
	return infos, nil
}

func (p *ZkPlugin) SearchNotes(query string, limit int) ([]contract.NoteInfo, error) {
	notes := p.vault.Search(context.Background(), query, limit)
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].Path < notes[j].Path
	})
	infos := make([]contract.NoteInfo, len(notes))
	for i, n := range notes {
		infos[i] = noteToContractInfo(n)
	}
	return infos, nil
}

func (p *ZkPlugin) ReloadVault() error {
	if p.vault.indexer != nil {
		p.vault.indexer.Close()
	}

	v, err := Open(p.vault.root, p.vault.indexPath, p.log)
	if err != nil {
		return err
	}

	p.vault = v
	return nil
}

// --- Helpers ---

func (p *ZkPlugin) resolveNote(ref string) (*Note, error) {
	if filepath.IsAbs(ref) {
		if n, ok := p.vault.NoteByPath(ref); ok {
			return n, nil
		}
		return nil, fmt.Errorf("note not found at path %q", ref)
	}
	if n, ok := p.vault.NoteByName(ref); ok {
		return n, nil
	}
	stem := strings.TrimSuffix(ref, filepath.Ext(ref))
	if n, ok := p.vault.NoteByName(stem); ok {
		return n, nil
	}
	return nil, fmt.Errorf("note %q not found in vault", ref)
}

func noteToContractInfo(n *Note) contract.NoteInfo {
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	wikilinks := n.Wikilinks
	if wikilinks == nil {
		wikilinks = []string{}
	}
	return contract.NoteInfo{
		Name:        n.Name,
		Path:        n.Path,
		Tags:        tags,
		Wikilinks:   wikilinks,
		Frontmatter: n.Frontmatter,
	}
}

func main() {
	impl := &ZkPlugin{}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"zk": &contract.ZkIntegrationPlugin{Impl: impl},
		},
	})
}
