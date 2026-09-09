// Package webui provides a browser-based management UI for the Clara agent.
// It serves at /ui/ on the HTTP server, rendering server-side HTML using Templ,
// styled with Tailwind CSS v4, DaisyUI v5 (Material 3 themed), and enhanced with HTMX and Alpine.js.
//
//go:generate templ generate -path ./templ
package webui

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/a-h/templ"
	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/webui/manifest"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
)

//go:embed all:dist
var distEmbedFS embed.FS

// EvaluatorInspector provides access to discovered actuators and fast-path heuristics.
type EvaluatorInspector interface {
	AutomationsOverview(ctx context.Context) ([]supervisor.AutomationSummary, error)
}

// IntegrationLister is implemented by *pluginLoader in cmd/clara.
type IntegrationLister interface {
	List() []map[string]any
}

// WebUI is the Clara management UI.
type WebUI struct {
	cfg       *config.Config
	cfgPath   string
	sup       *supervisor.Supervisor
	reg       *registry.Registry
	integ     IntegrationLister
	evaluator EvaluatorInspector
	approvals *supervisor.ApprovalStore
	log       zerolog.Logger
	manifest  manifest.Manifest
	isDev     bool
	devHost   string
}

// New constructs a WebUI.
func New(
	cfg *config.Config,
	cfgPath string,
	sup *supervisor.Supervisor,
	reg *registry.Registry,
	integ IntegrationLister,
	evaluator EvaluatorInspector,
	approvals *supervisor.ApprovalStore,
	log zerolog.Logger,
) *WebUI {
	isDev := strings.ToLower(os.Getenv("ENV")) == "dev" || strings.ToLower(os.Getenv("CLARA_ENV")) == "dev"
	devHost := os.Getenv("VITE_DEV_HOST")
	if devHost == "" {
		devHost = "http://localhost:3001"
	}

	var m manifest.Manifest
	if !isDev {
		manifestPath := os.Getenv("VITE_MANIFEST_PATH")
		if manifestPath == "" {
			manifestPath = "./dist/.vite/manifest.json"
		}
		var err error
		m, err = manifest.ReadManifest(manifestPath)
		if err != nil {
			// Try reading from embedded filesystem
			if raw, embedErr := distEmbedFS.ReadFile("dist/.vite/manifest.json"); embedErr == nil {
				m, _ = manifest.ParseManifest(raw)
			} else {
				log.Warn().Err(err).Str("path", manifestPath).Msg("failed to read vite manifest; UI assets might not load")
			}
		}
	}

	return &WebUI{
		cfg:       cfg,
		cfgPath:   cfgPath,
		sup:       sup,
		reg:       reg,
		integ:     integ,
		evaluator: evaluator,
		approvals: approvals,
		log:       log.With().Str("component", "webui").Logger(),
		manifest:  m,
		isDev:     isDev,
		devHost:   devHost,
	}
}

// Mount registers the UI routes onto mux and adds a redirect from / to /ui/.
func (w *WebUI) Mount(mux *http.ServeMux) {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Request logger
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

	// Static assets from ./dist (or embedded dist)
	subDist, _ := fs.Sub(distEmbedFS, "dist")
	distFileServer := http.FileServer(http.FS(subDist))

	e.GET("/assets/*", func(c echo.Context) error {
		// Prefer local dist on disk if available, otherwise embedded
		if _, err := os.Stat("." + c.Request().URL.Path); err == nil {
			http.ServeFile(c.Response().Writer, c.Request(), "."+c.Request().URL.Path)
			return nil
		}
		distFileServer.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	})
	e.GET("/favicon.svg", func(c echo.Context) error {
		if _, err := os.Stat("./public/favicon.svg"); err == nil {
			http.ServeFile(c.Response().Writer, c.Request(), "./public/favicon.svg")
			return nil
		}
		c.Request().URL.Path = "/favicon.svg"
		distFileServer.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	})

	// Page routes
	e.GET("/ui", func(c echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/ui/")
	})
	e.GET("/ui/", w.handleDashboard)
	e.GET("/ui/actuators", w.handleActuatorsList)
	e.GET("/ui/actuators/:id", w.handleActuatorDetail)
	e.POST("/ui/actuators/:id/run", w.handleActuatorRun)
	e.GET("/ui/approvals", w.handleApprovalsList)
	e.POST("/ui/approvals/:id/decide", w.handleApprovalDecide)
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
		http.NotFound(rw, r)
	})
	mux.Handle("/ui/", e)
	mux.Handle("/assets/", e)
	mux.Handle("/images/", e)
	mux.Handle("/favicon.svg", e)
}

// baseVM returns a initialized BaseVM for rendering pages.
func (w *WebUI) baseVM(pageTitle, location string) ui.BaseVM {
	return ui.NewBaseVM(w.manifest, w.isDev, w.devHost, pageTitle, location)
}

// render is a helper that renders a templ component into an Echo response.
func render(c echo.Context, status int, t templ.Component) error {
	c.Response().Header().Set(echo.HeaderContentType, "text/html; charset=utf-8")
	c.Response().WriteHeader(status)
	return t.Render(c.Request().Context(), c.Response())
}
