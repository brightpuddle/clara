package webui

import (
	"encoding/json"
	"strings"

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

func buildStructuredConfig(m map[string]any) *StructuredConfig {
	sc := &StructuredConfig{
		LogLevel: formatStr(m["log_level"]),
		DataDir:  formatStr(m["data_dir"]),
	}
	if sc.LogLevel == "" {
		sc.LogLevel = "info"
	}
	if v, ok := m["builder_repo_root"].(string); ok {
		sc.BuilderRepoRoot = v
	}
	if v, ok := m["mcp_startup_timeout"].(string); ok {
		sc.MCPStartupTimeout = v
	}

	if srv, ok := m["server"].(map[string]any); ok {
		sc.Server.ListenAddr = formatStr(srv["listen_addr"])
		sc.Server.SharedSecret = formatStr(srv["shared_secret"])
	}

	if notif, ok := m["notify"].(map[string]any); ok {
		sc.Notify.Backend = formatStr(notif["backend"])
		if wx, ok := notif["webex"].(map[string]any); ok {
			sc.Notify.Webex.RoomID = formatStr(wx["room_id"])
		}
		if dc, ok := notif["discord"].(map[string]any); ok {
			sc.Notify.Discord.ChannelID = formatStr(dc["channel_id"])
		}
	}

	if td, ok := m["task_dirs"].([]any); ok {
		for _, d := range td {
			if s, ok := d.(string); ok {
				sc.TaskDirs = append(sc.TaskDirs, s)
			}
		}
	}

	if plugs, ok := m["plugins"].([]any); ok {
		for _, p := range plugs {
			if pm, ok := p.(map[string]any); ok {
				sc.Plugins = append(sc.Plugins, PluginPayload{
					Name: formatStr(pm["name"]),
					Path: formatStr(pm["path"]),
				})
			}
		}
	}

	if psp, ok := m["plugin_search_paths"].([]any); ok {
		for _, p := range psp {
			if s, ok := p.(string); ok {
				sc.PluginSearchPaths = append(sc.PluginSearchPaths, s)
			}
		}
	}

	if mcpsp, ok := m["mcp_command_search_paths"].([]any); ok {
		for _, p := range mcpsp {
			if s, ok := p.(string); ok {
				sc.MCPCommandSearchPaths = append(sc.MCPCommandSearchPaths, s)
			}
		}
	}

	if srvs, ok := m["mcp_servers"].([]any); ok {
		for _, s := range srvs {
			if sm, ok := s.(map[string]any); ok {
				payload := MCPServerPayload{
					Name:        formatStr(sm["name"]),
					Command:     formatStr(sm["command"]),
					URL:         formatStr(sm["url"]),
					Token:       formatStr(sm["token"]),
					Description: formatStr(sm["description"]),
				}
				if payload.URL != "" {
					payload.ServerType = "http"
				} else {
					payload.ServerType = "command"
				}
				if sv, ok := sm["skip_verify"].(bool); ok {
					payload.SkipVerify = sv
				}
				if envMap, ok := sm["env"].(map[string]any); ok {
					for k, v := range envMap {
						payload.Env = append(payload.Env, EnvPair{Key: k, Value: formatStr(v)})
					}
				}
				sc.MCPServers = append(sc.MCPServers, payload)
			}
		}
	}

	knownKeys := map[string]bool{"db": true, "zk": true, "webex": true, "discord": true}
	if integ, ok := m["integrations"].(map[string]any); ok {
		if db, ok := integ["db"].(map[string]any); ok {
			sc.Integrations.DB.Path = formatStr(db["path"])
		}
		if zk, ok := integ["zk"].(map[string]any); ok {
			sc.Integrations.ZK.VaultRoot = formatStr(zk["vault_root"])
		}
		if wx, ok := integ["webex"].(map[string]any); ok {
			sc.Integrations.Webex.ClientID = formatStr(wx["client_id"])
			sc.Integrations.Webex.ClientSecret = formatStr(wx["client_secret"])
			sc.Integrations.Webex.BotToken = formatStr(wx["bot_token"])
			sc.Integrations.Webex.WebhookSecret = formatStr(wx["webhook_secret"])
			sc.Integrations.Webex.BaseURL = formatStr(wx["base_url"])
			sc.Integrations.Webex.Secret = formatStr(wx["secret"])
		}
		if dc, ok := integ["discord"].(map[string]any); ok {
			sc.Integrations.Discord.Token = formatStr(dc["token"])
			sc.Integrations.Discord.Secret = formatStr(dc["secret"])
			sc.Integrations.Discord.EveURL = formatStr(dc["eve_url"])
			sc.Integrations.Discord.Machine = formatStr(dc["machine"])
		}
		for k, v := range integ {
			if !knownKeys[k] {
				sc.CustomIntegrations = append(sc.CustomIntegrations, CustomIntegPayload{
					Name: k,
					YAML: formatAny(v),
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
