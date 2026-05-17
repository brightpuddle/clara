package webui

import (
	"net/http"
	"os"

	"github.com/brightpuddle/clara/internal/config"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

// handleConfigGet renders the YAML configuration editor.
func (w *WebUI) handleConfigGet(c echo.Context) error {
	return w.renderConfig(c, "", "")
}

// handleConfigPost handles the form submission to save config.
func (w *WebUI) handleConfigPost(c echo.Context) error {
	if w.cfgPath == "" {
		return w.renderConfig(c, "error", "No config file path configured")
	}

	rawYAML := c.FormValue("yaml")

	// Validate by parsing
	var tmp any
	if err := yaml.Unmarshal([]byte(rawYAML), &tmp); err != nil {
		return w.renderConfig(c, "error", "Invalid YAML: "+err.Error())
	}

	// Write file
	if err := os.WriteFile(w.cfgPath, []byte(rawYAML), 0o644); err != nil {
		return w.renderConfig(c, "error", "Save failed: "+err.Error())
	}

	return w.renderConfig(c, "success", "Configuration saved. Restart the agent to apply changes.")
}

func (w *WebUI) renderConfig(c echo.Context, flashKind, flash string) error {
	readOnly := w.cfgPath == ""
	var yamlStr string
	if w.cfgPath != "" {
		data, err := os.ReadFile(w.cfgPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			yamlStr = string(data)
		} else {
			// File doesn't exist yet — show empty template
			defaultCfg := &config.Config{}
			b, _ := yaml.Marshal(defaultCfg)
			yamlStr = string(b)
		}
	}
	return render(c, http.StatusOK, ui.Config(ui.ConfigData{
		YAML:      yamlStr,
		Flash:     flash,
		FlashKind: flashKind,
		ReadOnly:  readOnly,
	}))
}
