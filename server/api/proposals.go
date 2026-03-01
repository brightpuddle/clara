package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/brightpuddle/clara/internal/item"
	"github.com/brightpuddle/clara/server/db"
	"github.com/go-chi/chi/v5"
)

// proposalsRouter returns routes for the proposals sub-resource.
// Proposals are an abstraction over suggestions, returned as ClaraItems.
func (h *Handler) proposalsRouter(r chi.Router) {
	r.Get("/", h.listProposals)
	r.Post("/{id}/approve", h.approveProposal)
	r.Post("/{id}/dismiss", h.dismissProposal)
}

// listProposals returns pending suggestions as ClaraItems.
func (h *Handler) listProposals(w http.ResponseWriter, r *http.Request) {
	suggestions, err := h.db.ListSuggestions(r.Context(), "pending")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]item.ClaraItem, len(suggestions))
	for i, s := range suggestions {
		items[i] = suggestionToClaraItem(s)
	}
	writeJSON(w, http.StatusOK, items)
}

// approveProposal is an alias for approveSuggestion under the proposals path.
func (h *Handler) approveProposal(w http.ResponseWriter, r *http.Request) {
	h.updateStatus(w, r, "approved")
}

// dismissProposal sets status to "dismissed" (softer than "rejected").
func (h *Handler) dismissProposal(w http.ResponseWriter, r *http.Request) {
	h.updateStatus(w, r, "dismissed")
}

// suggestionToClaraItem converts a db.Suggestion to an item.ClaraItem.
func suggestionToClaraItem(s db.Suggestion) item.ClaraItem {
	src := filepath.Base(s.SourcePath)
	src = strings.TrimSuffix(src, filepath.Ext(src))

	surface := s.ActionSurface
	if surface == "" {
		surface = item.SurfaceLocalMac
	}

	body := buildSuggestionBody(src, s)

	return item.ClaraItem{
		ID:            fmt.Sprintf("suggestion-%d", s.ID),
		Type:          item.TypeSuggestion,
		Source:        item.SourceMarkdown,
		SourceRef:     s.SourcePath,
		Status:        item.StatusProposed,
		ActionSurface: surface,
		Created:       s.CreatedAt,
		Body:          body,
	}
}

func buildSuggestionBody(src string, s db.Suggestion) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Add backlink [[%s]] to `%s`\n\n", s.TargetTitle, src)
	fmt.Fprintf(&sb, "Similarity: %.0f%%\n", s.Similarity*100)
	if s.Context != "" {
		fmt.Fprintf(&sb, "\n> %s\n", s.Context)
	}
	fmt.Fprintf(&sb, "\nActions: approve · dismiss")
	return sb.String()
}
