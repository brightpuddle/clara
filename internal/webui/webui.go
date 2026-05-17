// Package webui provides a browser-based management UI for the Clara agent.
// It serves at /ui/ on the same HTTP server used for MCP and webhooks, so no
// additional port is needed.
//
// The UI is built with Echo (router), Templ (SSR templates), HTMX (partial
// updates), and Tailwind CSS + DaisyUI (styling).
//
//go:generate go run github.com/a-h/templ/cmd/templ@latest generate -f ./templ
package webui

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/a-h/templ"
	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/intentlog"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
)

//go:embed static
var staticFiles embed.FS

// IntegrationLister is implemented by *pluginLoader in cmd/clara.
type IntegrationLister interface {
	List() []map[string]any
}

// IntentSupervisor is implemented by *supervisor.Supervisor.
type IntentSupervisor interface {
	IntentInfos() []supervisor.IntentInfo
	Intent(id string) (*orchestrator.Intent, bool)
	StartIntent(id, task string) error
}

// WebUI is the Clara management UI. Call Mount to attach its routes to an
// existing net/http mux.
type WebUI struct {
	cfg     *config.Config
	cfgPath string // writable config file path
	sup     IntentSupervisor
	reg     *registry.Registry
	integ   IntegrationLister
	ilog    *intentlog.Logger
	log     zerolog.Logger
}

// New constructs a WebUI. cfgPath is the writable config file path used by the
// config editor. When empty, saving is disabled.
func New(
	cfg *config.Config,
	cfgPath string,
	sup IntentSupervisor,
	reg *registry.Registry,
	integ IntegrationLister,
	ilog *intentlog.Logger,
	log zerolog.Logger,
) *WebUI {
	return &WebUI{
		cfg:     cfg,
		cfgPath: cfgPath,
		sup:     sup,
		reg:     reg,
		integ:   integ,
		ilog:    ilog,
		log:     log.With().Str("component", "webui").Logger(),
	}
}

// Mount registers the UI routes onto mux and adds a redirect from / to /ui/.
func (w *WebUI) Mount(mux *http.ServeMux) {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Request logger using zerolog
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod: true,
		LogURI:    true,
		LogStatus: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			w.log.Debug().
				Str("method", v.Method).
				Str("uri", v.URI).
				Int("status", v.Status).
				Msg("web ui request")
			return nil
		},
	}))
	e.Use(middleware.Recover())

	// Static files (htmx, app.js, app.css)
	sub, _ := fs.Sub(staticFiles, "static")
	e.GET("/ui/static/*", echo.WrapHandler(
		http.StripPrefix("/ui/static/", http.FileServer(http.FS(sub))),
	))

	// Pages
	e.GET("/ui", func(c echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/ui/")
	})
	e.GET("/ui/", w.handleDashboard)
	e.GET("/ui/intents", w.handleIntentList)
	e.GET("/ui/intents/:id", w.handleIntentDetail)
	e.POST("/ui/intents/:id/run", w.handleIntentRun)
	e.GET("/ui/integrations", w.handleIntegrations)
	e.GET("/ui/logs", w.handleLogs)
	e.GET("/ui/logs/stream", w.handleLogsStream)
	e.GET("/ui/config", w.handleConfigGet)
	e.POST("/ui/config", w.handleConfigPost)

	// Mount into the parent mux
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(rw, r, "/ui/", http.StatusMovedPermanently)
			return
		}
		// Fall through for /mcp, /api, /auth, /events — not handled here
		http.NotFound(rw, r)
	})
	mux.Handle("/ui/", e)
	mux.Handle("/ui/static/", e)
}

// render is a helper that renders a templ component into an Echo response.
func render(c echo.Context, status int, t templ.Component) error {
	c.Response().Header().Set(echo.HeaderContentType, "text/html; charset=utf-8")
	c.Response().WriteHeader(status)
	return t.Render(c.Request().Context(), c.Response())
}
