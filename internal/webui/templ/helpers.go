package templ

import (
	"encoding/json"
	"strconv"
)

// itoa converts an int to a string for use inside templ expressions.
func itoa(n int) string {
	return strconv.Itoa(n)
}

// strVal extracts a string from a map[string]any value, returning "" if
// the value is absent or not a string.
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// configAlpineData creates the Alpine.js data object literal for the config editor.
func configAlpineData(configJSON, rawYAML string) string {
	if configJSON == "" {
		configJSON = "{}"
	}
	rawEscaped, _ := json.Marshal(rawYAML)
	return `{
		tab: 'structured',
		cfg: ` + configJSON + `,
		rawYaml: ` + string(rawEscaped) + `,
		ensureArrays() {
			if (!this.cfg.task_dirs) this.cfg.task_dirs = [];
			if (!this.cfg.plugins) this.cfg.plugins = [];
			if (!this.cfg.plugin_search_paths) this.cfg.plugin_search_paths = [];
			if (!this.cfg.mcp_command_search_paths) this.cfg.mcp_command_search_paths = [];
			if (!this.cfg.mcp_servers) this.cfg.mcp_servers = [];
			if (!this.cfg.custom_integrations) this.cfg.custom_integrations = [];
			if (!this.cfg.server) this.cfg.server = { listen_addr: '', shared_secret: '' };
			if (!this.cfg.notify) this.cfg.notify = { backend: 'dummy', webex: { room_id: '' }, discord: { channel_id: '' } };
			if (!this.cfg.notify.webex) this.cfg.notify.webex = { room_id: '' };
			if (!this.cfg.notify.discord) this.cfg.notify.discord = { channel_id: '' };
			if (!this.cfg.integrations) this.cfg.integrations = { db: { path: '' }, zk: { vault_root: '' }, webex: {}, discord: {} };
			if (!this.cfg.integrations.db) this.cfg.integrations.db = { path: '' };
			if (!this.cfg.integrations.zk) this.cfg.integrations.zk = { vault_root: '' };
			if (!this.cfg.integrations.webex) this.cfg.integrations.webex = { client_id: '', client_secret: '', bot_token: '', webhook_secret: '', base_url: '', secret: '' };
			if (!this.cfg.integrations.discord) this.cfg.integrations.discord = { token: '', secret: '', eve_url: '', machine: '' };
		},
		init() {
			this.ensureArrays();
		}
	}`
}
