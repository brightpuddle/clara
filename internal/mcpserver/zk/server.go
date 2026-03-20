package zk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Description is the human-readable summary shown in clara tool list.
const Description = "Built-in Zettelkasten MCP server: CRUD notes, resolve wikilinks, query by tag, and parse YAML frontmatter from Obsidian-style Markdown vaults."

// New creates a configured MCP server for the given vault root path.
func New(vaultRoot string) (*server.MCPServer, error) {
	v, err := Open(vaultRoot)
	if err != nil {
		return nil, err
	}

	s := server.NewMCPServer(
		"clara-zk",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	h := &handlers{vault: v}

	s.AddTool(mcp.NewTool("note_list",
		mcp.WithDescription(
			"List all notes in the vault. Returns name, path, tags, wikilinks, and frontmatter for each note.",
		),
	), h.handleNoteList)

	s.AddTool(mcp.NewTool("note_get",
		mcp.WithDescription(
			"Get the full content and parsed metadata of a note by name or path.",
		),
		mcp.WithString("note",
			mcp.Required(),
			mcp.Description(
				"Note name (filename stem, case-insensitive) or absolute path to the .md file.",
			),
		),
	), h.handleNoteGet)

	s.AddTool(mcp.NewTool("note_create",
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
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("Full Markdown content for the note, including optional YAML frontmatter."),
		),
	), h.handleNoteCreate)

	s.AddTool(mcp.NewTool("note_update",
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
	), h.handleNoteUpdate)

	s.AddTool(mcp.NewTool("note_delete",
		mcp.WithDescription("Permanently delete a note from the vault."),
		mcp.WithString("note",
			mcp.Required(),
			mcp.Description("Note name (case-insensitive) or absolute path to the .md file."),
		),
	), h.handleNoteDelete)

	s.AddTool(mcp.NewTool("note_resolve_wikilink",
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
	), h.handleResolveWikilink)

	s.AddTool(mcp.NewTool("tag_list",
		mcp.WithDescription(
			"List all tags present in the vault, with the count of notes that carry each tag.",
		),
	), h.handleTagList)

	s.AddTool(mcp.NewTool("tag_notes",
		mcp.WithDescription(
			"Return all notes that carry a given tag. "+
				"The leading # is optional. Tag matching is case-insensitive.",
		),
		mcp.WithString("tag",
			mcp.Required(),
			mcp.Description("Tag to search for, e.g. \"project\" or \"#project\"."),
		),
	), h.handleTagNotes)

	s.AddTool(mcp.NewTool("vault_reload",
		mcp.WithDescription(
			"Rebuild the vault index from disk. "+
				"Call this if you have made changes to the vault outside of this server.",
		),
	), h.handleVaultReload)

	return s, nil
}

// handlers holds the vault and exposes all tool handlers.
type handlers struct {
	vault *Vault
}

// ── noteInfo is the JSON shape shared by listing tools ───────────────────────

type noteInfo struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	Tags        []string       `json:"tags"`
	Wikilinks   []string       `json:"wikilinks"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
}

func noteToInfo(n *Note) noteInfo {
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	wikilinks := n.Wikilinks
	if wikilinks == nil {
		wikilinks = []string{}
	}
	return noteInfo{
		Name:        n.Name,
		Path:        n.Path,
		Tags:        tags,
		Wikilinks:   wikilinks,
		Frontmatter: n.Frontmatter,
	}
}

// ── Tool handlers ─────────────────────────────────────────────────────────────

func (h *handlers) handleNoteList(
	_ context.Context,
	_ mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	notes := h.vault.AllNotes()
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].Path < notes[j].Path
	})
	infos := make([]noteInfo, len(notes))
	for i, n := range notes {
		infos[i] = noteToInfo(n)
	}
	return jsonResult(infos)
}

func (h *handlers) handleNoteGet(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	ref, err := stringArg(req, "note")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	note, err := h.resolveNote(ref)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	content, err := os.ReadFile(note.Path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("note_get: read file: %v", err)), nil
	}

	result := map[string]any{
		"name":        note.Name,
		"path":        note.Path,
		"content":     string(content),
		"tags":        note.Tags,
		"wikilinks":   note.Wikilinks,
		"frontmatter": note.Frontmatter,
	}
	return jsonResult(result)
}

func (h *handlers) handleNoteCreate(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	name, err := stringArg(req, "name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	content, err := stringArg(req, "content")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Ensure the name has a .md extension for path construction, but strip it
	// first so the stem name is clean.
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	relPath := stem + ".md"
	absPath := filepath.Join(h.vault.Root(), relPath)

	if _, existing := h.vault.NoteByPath(absPath); existing {
		return mcp.NewToolResultError(
			fmt.Sprintf("note_create: note %q already exists", stem),
		), nil
	}
	// Also guard against the file existing on disk even if not indexed.
	if _, statErr := os.Stat(absPath); statErr == nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("note_create: file already exists at %q", absPath),
		), nil
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("note_create: mkdir: %v", err)), nil
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("note_create: write: %v", err)), nil
	}
	if err := h.vault.IndexPath(absPath); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("note_create: index: %v", err)), nil
	}

	note, _ := h.vault.NoteByPath(absPath)
	return jsonResult(map[string]any{
		"status": "created",
		"name":   stem,
		"path":   absPath,
		"tags":   note.Tags,
	})
}

func (h *handlers) handleNoteUpdate(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	ref, err := stringArg(req, "note")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	content, err := stringArg(req, "content")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	note, err := h.resolveNote(ref)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if writeErr := os.WriteFile(note.Path, []byte(content), 0o644); writeErr != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("note_update: write: %v", writeErr),
		), nil
	}
	if indexErr := h.vault.IndexPath(note.Path); indexErr != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("note_update: index: %v", indexErr),
		), nil
	}
	return mcp.NewToolResultText("updated"), nil
}

func (h *handlers) handleNoteDelete(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	ref, err := stringArg(req, "note")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	note, err := h.resolveNote(ref)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if removeErr := os.Remove(note.Path); removeErr != nil {
		return mcp.NewToolResultError(
			fmt.Sprintf("note_delete: remove: %v", removeErr),
		), nil
	}
	h.vault.RemovePath(note.Path)
	return mcp.NewToolResultText("deleted"), nil
}

func (h *handlers) handleResolveWikilink(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	target, err := stringArg(req, "target")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	resolved := h.vault.ResolveWikilink(target)
	return jsonResult(map[string]any{
		"target": target,
		"path":   resolved,
		"found":  resolved != "",
	})
}

func (h *handlers) handleTagList(
	_ context.Context,
	_ mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	type tagEntry struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}

	notes := h.vault.AllNotes()
	counts := make(map[string]int)
	for _, n := range notes {
		for _, t := range n.Tags {
			counts[t]++
		}
	}

	entries := make([]tagEntry, 0, len(counts))
	for tag, count := range counts {
		entries = append(entries, tagEntry{Tag: tag, Count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count != entries[j].Count {
			return entries[i].Count > entries[j].Count
		}
		return entries[i].Tag < entries[j].Tag
	})
	return jsonResult(entries)
}

func (h *handlers) handleTagNotes(
	_ context.Context,
	req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	tag, err := stringArg(req, "tag")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	notes := h.vault.NotesByTag(tag)
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].Path < notes[j].Path
	})
	infos := make([]noteInfo, len(notes))
	for i, n := range notes {
		infos[i] = noteToInfo(n)
	}
	return jsonResult(infos)
}

func (h *handlers) handleVaultReload(
	_ context.Context,
	_ mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	v, err := Open(h.vault.Root())
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("vault_reload: %v", err)), nil
	}
	h.vault = v
	count := len(h.vault.AllNotes())
	return mcp.NewToolResultText(fmt.Sprintf("reloaded %d notes", count)), nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// resolveNote resolves a user-supplied "note or path" reference.
// It accepts:
//   - an absolute path ending in .md
//   - a filename stem (case-insensitive)
func (h *handlers) resolveNote(ref string) (*Note, error) {
	// Try as absolute path first.
	if filepath.IsAbs(ref) {
		if n, ok := h.vault.NoteByPath(ref); ok {
			return n, nil
		}
		return nil, fmt.Errorf("note not found at path %q", ref)
	}
	// Try as stem name.
	if n, ok := h.vault.NoteByName(ref); ok {
		return n, nil
	}
	// Try stripping .md suffix.
	stem := strings.TrimSuffix(ref, filepath.Ext(ref))
	if n, ok := h.vault.NoteByName(stem); ok {
		return n, nil
	}
	return nil, fmt.Errorf("note %q not found in vault", ref)
}

func stringArg(req mcp.CallToolRequest, name string) (string, error) {
	val, ok := req.GetArguments()[name]
	if !ok {
		return "", fmt.Errorf("missing required argument: %q", name)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	return s, nil
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
