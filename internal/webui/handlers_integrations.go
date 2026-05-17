package webui

import (
	"net/http"

	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

// handleIntegrations renders the integrations status page.
func (w *WebUI) handleIntegrations(c echo.Context) error {
	plugins := w.integ.List()

	// Build MCP server list from registry statuses.
	statuses := w.reg.ServerStatuses()
	mcpServers := make([]map[string]any, 0, len(statuses))
	for name, status := range statuses {
		mcpServers = append(mcpServers, map[string]any{
			"name":   name,
			"status": string(status),
		})
	}

	return render(c, http.StatusOK, ui.Integrations(ui.IntegrationsData{
		Plugins:    plugins,
		MCPServers: mcpServers,
	}))
}
