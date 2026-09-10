package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/config"
	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

// StructuredConfig represents the full editable structured configuration.
type StructuredConfig struct {
	LogLevel              string                 `json:"log_level"`
	DataDir               string                 `json:"data_dir"`
	BuilderRepoRoot       string                 `json:"builder_repo_root"`
	MCPStartupTimeout     string                 `json:"mcp_startup_timeout"`
	Server                ServerPayload          `json:"server"`
	Notify                NotifyPayload          `json:"notify"`
	TaskDirs              []string               `json:"task_dirs"`
	Plugins               []PluginPayload        `json:"plugins"`
	PluginSearchPaths     []string               `json:"plugin_search_paths"`
	MCPCommandSearchPaths []string               `json:"mcp_command_search_paths"`
	MCPServers            []MCPServerPayload     `json:"mcp_servers"`
	Integrations          StructuredIntegrations `json:"integrations"`
	CustomIntegrations    []CustomIntegPayload   `json:"custom_integrations"`
}

type ServerPayload struct {
	ListenAddr   string `json:"listen_addr"`
	SharedSecret string `json:"shared_secret"`
}

type NotifyPayload struct {
	Backend string        `json:"backend"`
	Webex   WebexNotify   `json:"webex"`
	Discord DiscordNotify `json:"discord"`
}

type WebexNotify struct {
	RoomID string `json:"room_id"`
}

type DiscordNotify struct {
	ChannelID string `json:"channel_id"`
}

type PluginPayload struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type EnvPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type MCPServerPayload struct {
	Name        string    `json:"name"`
	ServerType  string    `json:"server_type"` // "command" or "http"
	Command     string    `json:"command"`
	URL         string    `json:"url"`
	Token       string    `json:"token"`
	SkipVerify  bool      `json:"skip_verify"`
	Description string    `json:"description"`
	Env         []EnvPair `json:"env"`
}

type StructuredIntegrations struct {
	DB      DBIntegPayload      `json:"db"`
	ZK      ZKIntegPayload      `json:"zk"`
	Webex   WebexIntegPayload   `json:"webex"`
	Discord DiscordIntegPayload `json:"discord"`
}

type DBIntegPayload struct {
	Path string `json:"path"`
}

type ZKIntegPayload struct {
	VaultRoot string `json:"vault_root"`
}

type WebexIntegPayload struct {
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	BotToken      string `json:"bot_token"`
	WebhookSecret string `json:"webhook_secret"`
	BaseURL       string `json:"base_url"`
	Secret        string `json:"secret"`
}

type DiscordIntegPayload struct {
	Token   string `json:"token"`
	Secret  string `json:"secret"`
	EveURL  string `json:"eve_url"`
	Machine string `json:"machine"`
}

type CustomIntegPayload struct {
	Name string `json:"name"`
	YAML string `json:"yaml"`
}

// handleConfigGet renders the structured configuration editor.
func (w *WebUI) handleConfigGet(c echo.Context) error {
	return w.renderConfig(c, "", "")
}

// handleConfigPost handles saving the structured config or raw YAML.
func (w *WebUI) handleConfigPost(c echo.Context) error {
	if w.cfgPath == "" {
		return w.renderConfig(c, "error", "No config file path configured")
	}

	rawJSON := c.FormValue("config_json")
	rawYAML := c.FormValue("yaml")

	var finalYAML []byte

	if rawJSON != "" {
		var sc StructuredConfig
		if err := json.Unmarshal([]byte(rawJSON), &sc); err != nil {
			return w.renderConfig(c, "error", "Invalid structured data: "+err.Error())
		}

		outYAML, err := structuredToYAML(&sc)
		if err != nil {
			return w.renderConfig(c, "error", "Failed to generate YAML: "+err.Error())
		}
		finalYAML = outYAML
	} else if rawYAML != "" {
		var tmp any
		if err := yaml.Unmarshal([]byte(rawYAML), &tmp); err != nil {
			return w.renderConfig(c, "error", "Invalid YAML: "+err.Error())
		}
		finalYAML = []byte(rawYAML)
	} else {
		return w.renderConfig(c, "error", "No configuration data provided")
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(w.cfgPath), 0o750); err != nil {
		return w.renderConfig(c, "error", "Failed to create directory: "+err.Error())
	}

	// Write YAML to file
	if err := os.WriteFile(w.cfgPath, finalYAML, 0o644); err != nil {
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
			defaultCfg := &config.Config{}
			b, _ := yaml.Marshal(defaultCfg)
			yamlStr = string(b)
		}
	}

	sc := yamlToStructured(yamlStr)
	jsonBytes, _ := json.Marshal(sc)

	vm := &ui.ConfigVM{
		Base:       w.baseVM("Configuration", "/ui/config"),
		ConfigJSON: string(jsonBytes),
		YAML:       yamlStr,
		Flash:      flash,
		FlashKind:  flashKind,
		ReadOnly:   readOnly,
	}

	return render(c, http.StatusOK, ui.Config(vm))
}

func yamlToStructured(yamlStr string) *StructuredConfig {
	sc := &StructuredConfig{
		LogLevel:              "info",
		TaskDirs:              []string{},
		Plugins:               []PluginPayload{},
		PluginSearchPaths:     []string{},
		MCPCommandSearchPaths: []string{},
		MCPServers:            []MCPServerPayload{},
		CustomIntegrations:    []CustomIntegPayload{},
	}

	if strings.TrimSpace(yamlStr) == "" {
		return sc
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(yamlStr), &raw); err != nil {
		return sc
	}

	if v, ok := raw["log_level"].(string); ok && v != "" {
		sc.LogLevel = v
	}
	if v, ok := raw["data_dir"].(string); ok {
		sc.DataDir = v
	}
	if v, ok := raw["builder_repo_root"].(string); ok {
		sc.BuilderRepoRoot = v
	}
	if v, ok := raw["mcp_startup_timeout"]; ok && v != nil {
		switch t := v.(type) {
		case string:
			sc.MCPStartupTimeout = t
		case time.Duration:
			sc.MCPStartupTimeout = t.String()
		case int:
			sc.MCPStartupTimeout = (time.Duration(t) * time.Second).String()
		}
	}

	if srv, ok := raw["server"].(map[string]any); ok {
		if v, ok := srv["listen_addr"].(string); ok {
			sc.Server.ListenAddr = v
		}
		if v, ok := srv["shared_secret"].(string); ok {
			sc.Server.SharedSecret = v
		}
	}

	if notif, ok := raw["notify"].(map[string]any); ok {
		if v, ok := notif["backend"].(string); ok {
			sc.Notify.Backend = v
		}
		if wx, ok := notif["webex"].(map[string]any); ok {
			if v, ok := wx["room_id"].(string); ok {
				sc.Notify.Webex.RoomID = v
			}
		}
		if dc, ok := notif["discord"].(map[string]any); ok {
			if v, ok := dc["channel_id"].(string); ok {
				sc.Notify.Discord.ChannelID = v
			}
		}
	}

	if td, ok := raw["task_dirs"].([]any); ok {
		for _, item := range td {
			if s, ok := item.(string); ok && s != "" {
				sc.TaskDirs = append(sc.TaskDirs, s)
			}
		}
	}

	if pl, ok := raw["plugins"].([]any); ok {
		for _, item := range pl {
			if m, ok := item.(map[string]any); ok {
				name, _ := m["name"].(string)
				path, _ := m["path"].(string)
				if name != "" {
					sc.Plugins = append(sc.Plugins, PluginPayload{Name: name, Path: path})
				}
			}
		}
	}

	if psp, ok := raw["plugin_search_paths"].([]any); ok {
		for _, item := range psp {
			if s, ok := item.(string); ok && s != "" {
				sc.PluginSearchPaths = append(sc.PluginSearchPaths, s)
			}
		}
	}

	if mcpsp, ok := raw["mcp_command_search_paths"].([]any); ok {
		for _, item := range mcpsp {
			if s, ok := item.(string); ok && s != "" {
				sc.MCPCommandSearchPaths = append(sc.MCPCommandSearchPaths, s)
			}
		}
	}

	if ms, ok := raw["mcp_servers"].([]any); ok {
		for _, item := range ms {
			if m, ok := item.(map[string]any); ok {
				name, _ := m["name"].(string)
				desc, _ := m["description"].(string)
				cmd, _ := m["command"].(string)
				url, _ := m["url"].(string)
				tok, _ := m["token"].(string)
				skipV, _ := m["skip_verify"].(bool)

				sType := "command"
				if url != "" {
					sType = "http"
				}

				var envPairs []EnvPair
				if envMap, ok := m["env"].(map[string]any); ok {
					var keys []string
					for k := range envMap {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						envPairs = append(envPairs, EnvPair{
							Key:   k,
							Value: formatAny(envMap[k]),
						})
					}
				}

				sc.MCPServers = append(sc.MCPServers, MCPServerPayload{
					Name:        name,
					ServerType:  sType,
					Command:     cmd,
					URL:         url,
					Token:       tok,
					SkipVerify:  skipV,
					Description: desc,
					Env:         envPairs,
				})
			}
		}
	}

	if integ, ok := raw["integrations"].(map[string]any); ok {
		for k, v := range integ {
			subMap, isMap := v.(map[string]any)
			if !isMap {
				continue
			}

			switch k {
			case "db":
				if p, ok := subMap["path"].(string); ok {
					sc.Integrations.DB.Path = p
				}
			case "zk":
				if vr, ok := subMap["vault_root"].(string); ok {
					sc.Integrations.ZK.VaultRoot = vr
				}
			case "webex":
				sc.Integrations.Webex.ClientID = formatStr(subMap["client_id"])
				sc.Integrations.Webex.ClientSecret = formatStr(subMap["client_secret"])
				sc.Integrations.Webex.BotToken = formatStr(subMap["bot_token"])
				sc.Integrations.Webex.WebhookSecret = formatStr(subMap["webhook_secret"])
				sc.Integrations.Webex.BaseURL = formatStr(subMap["base_url"])
				sc.Integrations.Webex.Secret = formatStr(subMap["secret"])
			case "discord":
				sc.Integrations.Discord.Token = formatStr(subMap["token"])
				sc.Integrations.Discord.Secret = formatStr(subMap["secret"])
				sc.Integrations.Discord.EveURL = formatStr(subMap["eve_url"])
				sc.Integrations.Discord.Machine = formatStr(subMap["machine"])
			default:
				b, _ := yaml.Marshal(subMap)
				sc.CustomIntegrations = append(sc.CustomIntegrations, CustomIntegPayload{
					Name: k,
					YAML: strings.TrimSpace(string(b)),
				})
			}
		}
	}

	return sc
}

func structuredToYAML(sc *StructuredConfig) ([]byte, error) {
	out := make(map[string]any)

	if sc.LogLevel != "" {
		out["log_level"] = sc.LogLevel
	}
	if sc.DataDir != "" {
		out["data_dir"] = sc.DataDir
	}
	if sc.BuilderRepoRoot != "" {
		out["builder_repo_root"] = sc.BuilderRepoRoot
	}
	if sc.MCPStartupTimeout != "" {
		out["mcp_startup_timeout"] = sc.MCPStartupTimeout
	}

	if sc.Server.ListenAddr != "" || sc.Server.SharedSecret != "" {
		srv := make(map[string]any)
		if sc.Server.ListenAddr != "" {
			srv["listen_addr"] = sc.Server.ListenAddr
		}
		if sc.Server.SharedSecret != "" {
			srv["shared_secret"] = sc.Server.SharedSecret
		}
		out["server"] = srv
	}

	notifyMap := make(map[string]any)
	if sc.Notify.Backend != "" {
		notifyMap["backend"] = sc.Notify.Backend
	}
	if sc.Notify.Webex.RoomID != "" {
		notifyMap["webex"] = map[string]any{"room_id": sc.Notify.Webex.RoomID}
	}
	if sc.Notify.Discord.ChannelID != "" {
		notifyMap["discord"] = map[string]any{"channel_id": sc.Notify.Discord.ChannelID}
	}
	if len(notifyMap) > 0 {
		out["notify"] = notifyMap
	}

	var taskDirs []string
	for _, d := range sc.TaskDirs {
		d = strings.TrimSpace(d)
		if d != "" {
			taskDirs = append(taskDirs, d)
		}
	}
	if len(taskDirs) > 0 {
		out["task_dirs"] = taskDirs
	}

	var plugins []map[string]string
	for _, p := range sc.Plugins {
		name := strings.TrimSpace(p.Name)
		if name != "" {
			item := map[string]string{"name": name}
			if strings.TrimSpace(p.Path) != "" {
				item["path"] = strings.TrimSpace(p.Path)
			}
			plugins = append(plugins, item)
		}
	}
	if len(plugins) > 0 {
		out["plugins"] = plugins
	}

	var psp []string
	for _, p := range sc.PluginSearchPaths {
		p = strings.TrimSpace(p)
		if p != "" {
			psp = append(psp, p)
		}
	}
	if len(psp) > 0 {
		out["plugin_search_paths"] = psp
	}

	var mcpsp []string
	for _, p := range sc.MCPCommandSearchPaths {
		p = strings.TrimSpace(p)
		if p != "" {
			mcpsp = append(mcpsp, p)
		}
	}
	if len(mcpsp) > 0 {
		out["mcp_command_search_paths"] = mcpsp
	}

	var mcpServers []map[string]any
	for _, s := range sc.MCPServers {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		item := map[string]any{"name": name}
		if s.Description != "" {
			item["description"] = s.Description
		}
		if s.ServerType == "http" || s.URL != "" {
			item["url"] = s.URL
			if s.Token != "" {
				item["token"] = s.Token
			}
			if s.SkipVerify {
				item["skip_verify"] = true
			}
		} else {
			item["command"] = s.Command
			envMap := make(map[string]string)
			for _, e := range s.Env {
				k := strings.TrimSpace(e.Key)
				if k != "" {
					envMap[k] = e.Value
				}
			}
			if len(envMap) > 0 {
				item["env"] = envMap
			}
		}
		mcpServers = append(mcpServers, item)
	}
	if len(mcpServers) > 0 {
		out["mcp_servers"] = mcpServers
	}

	integMap := make(map[string]any)
	if sc.Integrations.DB.Path != "" {
		integMap["db"] = map[string]any{"path": sc.Integrations.DB.Path}
	}
	if sc.Integrations.ZK.VaultRoot != "" {
		integMap["zk"] = map[string]any{"vault_root": sc.Integrations.ZK.VaultRoot}
	}

	webexMap := make(map[string]any)
	if sc.Integrations.Webex.ClientID != "" {
		webexMap["client_id"] = sc.Integrations.Webex.ClientID
	}
	if sc.Integrations.Webex.ClientSecret != "" {
		webexMap["client_secret"] = sc.Integrations.Webex.ClientSecret
	}
	if sc.Integrations.Webex.BotToken != "" {
		webexMap["bot_token"] = sc.Integrations.Webex.BotToken
	}
	if sc.Integrations.Webex.WebhookSecret != "" {
		webexMap["webhook_secret"] = sc.Integrations.Webex.WebhookSecret
	}
	if sc.Integrations.Webex.BaseURL != "" {
		webexMap["base_url"] = sc.Integrations.Webex.BaseURL
	}
	if sc.Integrations.Webex.Secret != "" {
		webexMap["secret"] = sc.Integrations.Webex.Secret
	}
	if len(webexMap) > 0 {
		integMap["webex"] = webexMap
	}

	discordMap := make(map[string]any)
	if sc.Integrations.Discord.Token != "" {
		discordMap["token"] = sc.Integrations.Discord.Token
	}
	if sc.Integrations.Discord.Secret != "" {
		discordMap["secret"] = sc.Integrations.Discord.Secret
	}
	if sc.Integrations.Discord.EveURL != "" {
		discordMap["eve_url"] = sc.Integrations.Discord.EveURL
	}
	if sc.Integrations.Discord.Machine != "" {
		discordMap["machine"] = sc.Integrations.Discord.Machine
	}
	if len(discordMap) > 0 {
		integMap["discord"] = discordMap
	}

	for _, ci := range sc.CustomIntegrations {
		name := strings.TrimSpace(ci.Name)
		if name == "" {
			continue
		}
		var customVal any
		if strings.TrimSpace(ci.YAML) != "" {
			if err := yaml.Unmarshal([]byte(ci.YAML), &customVal); err != nil {
				return nil, err
			}
			integMap[name] = customVal
		} else {
			integMap[name] = map[string]any{}
		}
	}
	if len(integMap) > 0 {
		out["integrations"] = integMap
	}

	return yaml.Marshal(out)
}

func formatStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func formatAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
