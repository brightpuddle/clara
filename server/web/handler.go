package web

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brightpuddle/clara/server/db"
	"github.com/go-chi/chi/v5"
)

// ProposalItem is the view model for a proposal in the web UI.
type ProposalItem struct {
	ID, Type, Source, Body, Status, ActionSurface string
}

// WebHandler serves the server-side rendered web UI.
type WebHandler struct {
	db        *db.DB
	startTime time.Time
}

// NewWebHandler creates a new WebHandler backed by the given DB.
func NewWebHandler(database *db.DB) *WebHandler {
	return &WebHandler{db: database, startTime: time.Now()}
}

// Router returns a chi router with all web UI routes.
func (h *WebHandler) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.dashboard)
	r.Get("/dashboard/stats", h.dashboardStats)
	r.Get("/proposals", h.proposals)
	r.Post("/proposals/{id}/approve", h.approveProposal)
	r.Post("/proposals/{id}/dismiss", h.dismissProposal)
	return r
}

func (h *WebHandler) dashboard(w http.ResponseWriter, r *http.Request) {
	counts, err := h.db.CountSuggestions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	uptime := time.Since(h.startTime).Round(time.Second).String()
	Dashboard(counts, uptime).Render(r.Context(), w)
}

func (h *WebHandler) dashboardStats(w http.ResponseWriter, r *http.Request) {
	counts, err := h.db.CountSuggestions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	DashboardStats(counts).Render(r.Context(), w)
}

func (h *WebHandler) proposals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	suggestions, err := h.db.ListSuggestions(r.Context(), status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]ProposalItem, len(suggestions))
	for i, s := range suggestions {
		items[i] = suggestionToProposalItem(s)
	}
	// HTMX partial: return just the list content for tab switching
	if r.Header.Get("HX-Request") == "true" {
		ProposalList(items, status).Render(r.Context(), w)
		return
	}
	Proposals(items, status).Render(r.Context(), w)
}

func (h *WebHandler) approveProposal(w http.ResponseWriter, r *http.Request) {
	h.updateProposalStatus(w, r, "approved")
}

func (h *WebHandler) dismissProposal(w http.ResponseWriter, r *http.Request) {
	h.updateProposalStatus(w, r, "dismissed")
}

func (h *WebHandler) updateProposalStatus(w http.ResponseWriter, r *http.Request, status string) {
	idParam := chi.URLParam(r, "id")
	numericStr := strings.TrimPrefix(idParam, "suggestion-")
	id, err := strconv.ParseInt(numericStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.db.UpdateSuggestionStatus(r.Context(), id, status); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		// Return a minimal actioned row (buttons replaced by status badge)
		ProposalRow(ProposalItem{ID: idParam, Status: status}).Render(r.Context(), w)
		return
	}
	http.Redirect(w, r, "/proposals", http.StatusSeeOther)
}

func suggestionToProposalItem(s db.Suggestion) ProposalItem {
	src := filepath.Base(s.SourcePath)
	src = strings.TrimSuffix(src, filepath.Ext(src))
	body := fmt.Sprintf("Add backlink [[%s]] to `%s` (similarity %.0f%%)", s.TargetTitle, src, s.Similarity*100)
	return ProposalItem{
		ID:            fmt.Sprintf("suggestion-%d", s.ID),
		Type:          s.Type,
		Source:        src,
		Body:          body,
		Status:        s.Status,
		ActionSurface: s.ActionSurface,
	}
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
