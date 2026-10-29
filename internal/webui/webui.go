// Package webui provides a browser-based management UI for the Clara agent.
// It serves a React SPA at /ui/ on the HTTP server, styled with Tailwind CSS v4 and shadcn/ui.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/trigger"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
)

//go:embed all:dist
var distEmbedFS embed.FS

// IntegrationLister is implemented by *pluginLoader in cmd/clara.
type IntegrationLister interface {
	List() []map[string]any
}

// WebUI is the Clara management UI.
type WebUI struct {
	cfg        *config.Config
	cfgPath    string
	sup        *supervisor.Supervisor
	reg        *registry.Registry
	integ      IntegrationLister
	triggerMgr *trigger.Manager
	db         *store.Store
	log        zerolog.Logger
}

// New constructs a WebUI.
func New(
	cfg *config.Config,
	cfgPath string,
	sup *supervisor.Supervisor,
	reg *registry.Registry,
	integ IntegrationLister,
	triggerMgr *trigger.Manager,
	db *store.Store,
	log zerolog.Logger,
) *WebUI {
	return &WebUI{
		cfg:        cfg,
		cfgPath:    cfgPath,
		sup:        sup,
		reg:        reg,
		integ:      integ,
		triggerMgr: triggerMgr,
		db:         db,
		log:        log.With().Str("component", "webui").Logger(),
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

	// Static assets from ./frontend/dist (or embedded dist)
	subDist, _ := fs.Sub(distEmbedFS, "dist")
	distFileServer := http.FileServer(http.FS(subDist))

	e.GET("/assets/*", func(c echo.Context) error {
		// Prefer local dist on disk if available, otherwise embedded
		candidates := []string{
			"./frontend/dist" + c.Request().URL.Path,
			"./dist" + c.Request().URL.Path,
			"." + c.Request().URL.Path,
		}
		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				http.ServeFile(c.Response().Writer, c.Request(), cand)
				return nil
			}
		}
		distFileServer.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	})

	e.GET("/favicon.svg", func(c echo.Context) error {
		candidates := []string{
			"./frontend/dist/favicon.svg",
			"./dist/favicon.svg",
			"./public/favicon.svg",
		}
		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				http.ServeFile(c.Response().Writer, c.Request(), cand)
				return nil
			}
		}
		c.Request().URL.Path = "/favicon.svg"
		distFileServer.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	})

	// REST API Routes
	api := e.Group("/ui/api")
	api.GET("/dashboard", w.handleAPIDashboard)
	api.GET("/triggers", w.handleAPITriggersList)
	api.POST("/triggers", w.handleAPITriggerCreate)
	api.GET("/triggers/:id", w.handleAPITriggerDetail)
	api.POST("/triggers/:id", w.handleAPITriggerUpdate)
	api.DELETE("/triggers/:id", w.handleAPITriggerDelete)
	api.POST("/triggers/:id/run", w.handleAPITriggerRun)
	api.GET("/runs", w.handleAPIRuns)
	api.GET("/integrations", w.handleAPIIntegrations)
	api.GET("/logs", w.handleAPILogs)
	api.GET("/config", w.handleAPIConfigGet)
	api.POST("/config", w.handleAPIConfigPost)

	// Log stream SSE
	e.GET("/ui/logs/stream", w.handleLogsStream)

	// Legacy form endpoints to support POST / DELETE actions smoothly
	e.POST("/ui/triggers", w.handleAPITriggerCreate)
	e.POST("/ui/triggers/:id", w.handleAPITriggerUpdate)
	e.POST("/ui/triggers/:id/run", w.handleAPITriggerRun)
	e.POST("/ui/triggers/:id/delete", w.handleAPITriggerDelete)
	e.DELETE("/ui/triggers/:id", w.handleAPITriggerDelete)
	e.POST("/ui/config", w.handleAPIConfigPost)

	// SPA Fallback Handler
	serveSPA := func(c echo.Context) error {
		candidates := []string{
			"./frontend/dist/index.html",
			"../frontend/dist/index.html",
			"./dist/index.html",
		}
		for _, p := range candidates {
			if abs, err := filepath.Abs(p); err == nil {
				if _, err := os.Stat(abs); err == nil {
					return c.File(abs)
				}
			}
		}
		// If embedded dist has index.html
		if data, err := distEmbedFS.ReadFile("dist/index.html"); err == nil {
			return c.HTMLBlob(http.StatusOK, data)
		}
		return c.String(http.StatusOK, "Clara Frontend build not found. Run 'make build' or 'cd frontend && pnpm run build'")
	}

	// SPA Page Routes
	e.GET("/ui", func(c echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/ui/")
	})
	e.GET("/ui/*", serveSPA)

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
	mux.Handle("/favicon.svg", e)
}
