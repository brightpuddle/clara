package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/brightpuddle/clara/server/db"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Handler struct {
	db         *db.DB
	startTime  time.Time
	webHandler http.Handler
}

func NewHandler(database *db.DB) *Handler {
	return &Handler{db: database, startTime: time.Now()}
}

// SetWebHandler mounts a web UI handler at the root path of the API router.
func (h *Handler) SetWebHandler(web http.Handler) {
	h.webHandler = web
}

// Router returns a chi router with all API routes mounted.
func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/suggestions", h.listSuggestions)
		r.Post("/suggestions/{id}/approve", h.approveSuggestion)
		r.Post("/suggestions/{id}/reject", h.rejectSuggestion)
		r.Get("/health", h.health)
		r.Get("/status", h.status)
		r.Route("/proposals", h.proposalsRouter)
	})

	if h.webHandler != nil {
		r.Mount("/", h.webHandler)
	}

	return r
}

func (h *Handler) listSuggestions(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	suggestions, err := h.db.ListSuggestions(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, suggestions)
}

func (h *Handler) approveSuggestion(w http.ResponseWriter, r *http.Request) {
	h.updateStatus(w, r, "approved")
}

func (h *Handler) rejectSuggestion(w http.ResponseWriter, r *http.Request) {
	h.updateStatus(w, r, "rejected")
}

func (h *Handler) updateStatus(w http.ResponseWriter, r *http.Request, status string) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.db.UpdateSuggestionStatus(r.Context(), id, status); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	counts, err := h.db.CountSuggestions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	docCount, _ := h.db.CountDocuments(r.Context()) // non-fatal if fails
	type statusResp struct {
		Status      string              `json:"status"`
		Uptime      string              `json:"uptime"`
		Documents   int                 `json:"documents"`
		Suggestions db.SuggestionCounts `json:"suggestions"`
	}
	writeJSON(w, http.StatusOK, statusResp{
		Status:      "ok",
		Uptime:      time.Since(h.startTime).Round(time.Second).String(),
		Documents:   docCount,
		Suggestions: counts,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
