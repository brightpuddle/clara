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

// triggerAlpineData creates the Alpine.js data object literal for the trigger editor.
func triggerAlpineData(triggerJSON, rawYAML string, isNew bool) string {
	if triggerJSON == "" {
		triggerJSON = "{}"
	}
	rawEscaped, _ := json.Marshal(rawYAML)
	isNewStr := "false"
	if isNew {
		isNewStr = "true"
	}
	return `{
		tab: 'structured',
		isNew: ` + isNewStr + `,
		trig: ` + triggerJSON + `,
		rawYaml: ` + string(rawEscaped) + `,
		argInput: '',
		addArg() {
			if (!this.trig.args) this.trig.args = [];
			if (this.argInput.trim()) {
				this.trig.args.push(this.argInput.trim());
				this.argInput = '';
			}
		},
		removeArg(index) {
			this.trig.args.splice(index, 1);
		},
		addEnv() {
			if (!this.trig.env) this.trig.env = [];
			this.trig.env.push({ key: '', value: '' });
		},
		removeEnv(index) {
			this.trig.env.splice(index, 1);
		},
		fillTemplate(type) {
			if (type === 'event') {
				this.trig.type = 'event';
				this.trig.rule_field = 'type';
				this.trig.rule_op = 'equals';
				this.trig.rule_value = 'custom.event';
				this.trig.exec = 'bun';
				this.trig.args = ['run', 'scripts/handle_event.ts'];
				this.trig.pass_event = 'stdin';
				this.trig.timeout = '30s';
			} else if (type === 'schedule') {
				this.trig.type = 'schedule';
				this.trig.schedule = '@every 10m';
				this.trig.exec = 'bun';
				this.trig.args = ['run', 'scripts/scheduled_task.ts'];
				this.trig.timeout = '60s';
			} else if (type === 'worker') {
				this.trig.type = 'worker';
				this.trig.restart = 'always';
				this.trig.restart_delay = '5s';
				this.trig.max_restarts = 0;
				this.trig.exec = 'bun';
				this.trig.args = ['run', 'workers/service.ts'];
			}
		},
		ensureDefaults() {
			if (!this.trig.args) this.trig.args = [];
			if (!this.trig.env) this.trig.env = [];
			if (!this.trig.type) this.trig.type = 'event';
			if (!this.trig.rule_op) this.trig.rule_op = 'equals';
			if (!this.trig.pass_event) this.trig.pass_event = 'stdin';
			if (!this.trig.restart) this.trig.restart = 'always';
			if (this.trig.enabled === undefined) this.trig.enabled = true;
		},
		init() {
			this.ensureDefaults();
		}
	}`
}
