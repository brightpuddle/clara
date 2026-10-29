package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/trigger"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

type mockIntegLister struct {
	list []map[string]any
}

func (m *mockIntegLister) List() []map[string]any {
	return m.list
}

func TestWebUI_Routes(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte("log_level: info\n"), 0o644)

	logPath := filepath.Join(tempDir, "clara.log")
	_ = os.WriteFile(logPath, []byte(`{"level":"info","time":"2026-09-08T00:00:00Z","message":"test log line"}`+"\n"), 0o644)

	dbPath := filepath.Join(tempDir, "clara.db")
	logger := zerolog.New(io.Discard)
	db, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	cfg := &config.Config{
		DataDir:  tempDir,
		LogLevel: "info",
	}

	reg := registry.New(logger)
	sup := supervisor.New(reg, nil, logger)
	eventBus := supervisor.NewEventBus()
	triggerMgr := trigger.NewManager(nil, eventBus, db)

	_ = triggerMgr.Register(trigger.Definition{
		ID:          "test-trigger",
		Description: "A test trigger for webui tests",
		Type:        trigger.TypeEvent,
		Match: &trigger.Rule{
			Field: "type",
			Op:    trigger.OpEquals,
			Value: "clara.test",
		},
		Action: trigger.Action{
			Exec: "echo test",
		},
	})

	integ := &mockIntegLister{
		list: []map[string]any{
			{"name": "test-plugin", "status": "running"},
		},
	}

	ui := New(cfg, cfgPath, sup, reg, integ, triggerMgr, db, logger)

	mux := http.NewServeMux()
	ui.Mount(mux)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := srv.Client()

	// Verify SPA HTML shell is served for page routes
	routes := []string{
		"/ui/",
		"/ui/triggers",
		"/ui/triggers/test-trigger",
		"/ui/runs",
		"/ui/integrations",
		"/ui/logs",
		"/ui/config",
	}

	for _, path := range routes {
		t.Run("SPA shell: "+path, func(t *testing.T) {
			resp, err := client.Get(srv.URL + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read response body: %v", err)
			}

			if !strings.Contains(string(body), "id=\"root\"") {
				t.Errorf("expected body to contain SPA root div, body:\n%s", string(body))
			}
		})
	}

	// Verify REST API endpoints return appropriate JSON
	apiTests := []struct {
		name     string
		path     string
		contains string
	}{
		{
			name:     "API Dashboard",
			path:     "/ui/api/dashboard",
			contains: "triggers_count",
		},
		{
			name:     "API Triggers",
			path:     "/ui/api/triggers",
			contains: "test-trigger",
		},
		{
			name:     "API Trigger Detail",
			path:     "/ui/api/triggers/test-trigger",
			contains: "test-trigger",
		},
		{
			name:     "API Runs",
			path:     "/ui/api/runs",
			contains: "runs",
		},
		{
			name:     "API Integrations",
			path:     "/ui/api/integrations",
			contains: "test-plugin",
		},
		{
			name:     "API Logs",
			path:     "/ui/api/logs",
			contains: "test log line",
		},
		{
			name:     "API Config",
			path:     "/ui/api/config",
			contains: "log_level",
		},
	}

	for _, tt := range apiTests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), tt.contains) {
				t.Errorf("expected API response to contain %q, got:\n%s", tt.contains, string(body))
			}
		})
	}
}

func TestHandleConfig_GetAndPostStructured(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")
	initialYAML := `log_level: info
data_dir: /tmp/test-data
task_dirs:
  - /tmp/tasks1
  - /tmp/tasks2
plugins:
  - name: chrome
  - name: macos
    path: /usr/local/bin/ClaraBridge
mcp_servers:
  - name: github
    command: github-mcp-server stdio
    description: GitHub tools
    env:
      GITHUB_TOKEN: secret123
integrations:
  db:
    path: /tmp/db.sqlite
  custom_plugin:
    foo: bar
`
	if err := os.WriteFile(cfgPath, []byte(initialYAML), 0o644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	logger := zerolog.New(io.Discard)
	dbPath := filepath.Join(tempDir, "clara.db")
	db, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	reg := registry.New(logger)
	sup := supervisor.New(reg, nil, logger)
	eventBus := supervisor.NewEventBus()
	triggerMgr := trigger.NewManager(nil, eventBus, db)
	integ := &mockIntegLister{}
	cfg := &config.Config{DataDir: tempDir, LogLevel: "info"}

	ui := New(cfg, cfgPath, sup, reg, integ, triggerMgr, db, logger)
	mux := http.NewServeMux()
	ui.Mount(mux)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := srv.Client()

	// 1. Test GET /ui/api/config
	resp, err := client.Get(srv.URL + "/ui/api/config")
	if err != nil {
		t.Fatalf("GET /ui/api/config failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "github-mcp-server") {
		t.Errorf("expected body to contain initial mcp server config")
	}

	// 2. Test POST /ui/api/config with structured JSON
	newStructuredJSON := `{
		"log_level": "debug",
		"data_dir": "/tmp/custom-data",
		"builder_repo_root": "/tmp/repo",
		"mcp_startup_timeout": "45s",
		"server": {
			"listen_addr": ":5555",
			"shared_secret": "my-secret"
		},
		"notify": {
			"backend": "discord",
			"webex": {"room_id": "r-123"},
			"discord": {"channel_id": "c-456"}
		},
		"task_dirs": [
			"/tmp/new-tasks"
		],
		"plugins": [
			{"name": "macos", "path": "/usr/local/bin/ClaraBridge"}
		],
		"plugin_search_paths": ["/opt/plugins"],
		"mcp_command_search_paths": ["/opt/mcp"],
		"mcp_servers": [
			{
				"name": "remote-api",
				"server_type": "http",
				"url": "https://mcp.example.com",
				"token": "tok123",
				"skip_verify": true,
				"description": "Remote MCP"
			}
		],
		"integrations": {
			"db": {"path": "/tmp/new-db.sqlite"},
			"zk": {"vault_root": "/vaults/main"},
			"webex": {
				"client_id": "wx-id",
				"client_secret": "wx-sec",
				"bot_token": "wx-tok",
				"webhook_secret": "wx-wh",
				"base_url": "https://webex.example.com",
				"secret": "wx-s"
			},
			"discord": {
				"token": "dc-tok",
				"secret": "dc-sec",
				"eve_url": "https://discord.example.com",
				"machine": "mac-mini"
			}
		},
		"custom_integrations": [
			{"name": "custom_plugin", "yaml": "foo: bar\nbaz: 123\n"}
		]
	}`

	postResp, err := client.Post(srv.URL+"/ui/api/config", "application/json", strings.NewReader(newStructuredJSON))
	if err != nil {
		t.Fatalf("POST /ui/api/config failed: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", postResp.StatusCode)
	}

	// Verify updated config on disk
	savedYAML, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read updated config file: %v", err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(savedYAML, &parsed); err != nil {
		t.Fatalf("saved config is invalid YAML: %v", err)
	}

	if parsed["log_level"] != "debug" {
		t.Errorf("expected log_level 'debug', got %v", parsed["log_level"])
	}
	if parsed["data_dir"] != "/tmp/custom-data" {
		t.Errorf("expected data_dir '/tmp/custom-data', got %v", parsed["data_dir"])
	}
}

func TestWebUI_TriggerCRUD(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte("log_level: info\n"), 0o644)

	logger := zerolog.New(io.Discard)
	dbPath := filepath.Join(tempDir, "clara.db")
	db, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	reg := registry.New(logger)
	sup := supervisor.New(reg, nil, logger)
	eventBus := supervisor.NewEventBus()
	triggerMgr := trigger.NewManager(nil, eventBus, db)
	integ := &mockIntegLister{}
	cfg := &config.Config{
		DataDir:  tempDir,
		LogLevel: "info",
	}

	ui := New(cfg, cfgPath, sup, reg, integ, triggerMgr, db, logger)
	mux := http.NewServeMux()
	ui.Mount(mux)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirects to check 303 status
		},
	}

	// 1. Test POST /ui/triggers (Structured JSON)
	createStructuredJSON := `{
		"id": "my-email-trigger",
		"name": "My Email Trigger",
		"description": "Handles inbox emails",
		"enabled": true,
		"type": "event",
		"rule_field": "type",
		"rule_op": "equals",
		"rule_value": "email.received",
		"exec": "lua scripts/email.lua",
		"pass_event": "stdin",
		"timeout": "30s",
		"args": [],
		"env": []
	}`
	form := url.Values{}
	form.Set("trigger_json", createStructuredJSON)

	postResp, err := client.PostForm(srv.URL+"/ui/triggers", form)
	if err != nil {
		t.Fatalf("POST /ui/triggers failed: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect (303), got %d", postResp.StatusCode)
	}

	// Verify trigger in manager
	def, exists := triggerMgr.Get("my-email-trigger")
	if !exists {
		t.Fatalf("trigger 'my-email-trigger' was not registered in manager")
	}
	if def.Name != "My Email Trigger" || def.Action.Exec != "lua scripts/email.lua" {
		t.Errorf("unexpected trigger def: %+v", def)
	}

	// Verify YAML file exists on disk
	filePath := triggerMgr.GetFilePath("my-email-trigger")
	if filePath == "" {
		t.Fatalf("expected file path for 'my-email-trigger'")
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("trigger file %s does not exist on disk: %v", filePath, err)
	}

	// 2. Test POST /ui/triggers/my-email-trigger (Update and rename ID)
	updateJSON := `{
		"id": "renamed-trigger",
		"name": "Renamed Trigger",
		"description": "Updated description",
		"enabled": true,
		"type": "schedule",
		"schedule": "*/5 * * * *",
		"exec": "python3 check.py",
		"timeout": "10s",
		"args": [],
		"env": []
	}`
	updateForm := url.Values{}
	updateForm.Set("trigger_json", updateJSON)

	updateResp, err := client.PostForm(srv.URL+"/ui/triggers/my-email-trigger", updateForm)
	if err != nil {
		t.Fatalf("POST /ui/triggers/my-email-trigger failed: %v", err)
	}
	updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect (303), got %d", updateResp.StatusCode)
	}

	// Old trigger should no longer exist
	if _, exists := triggerMgr.Get("my-email-trigger"); exists {
		t.Errorf("expected old trigger 'my-email-trigger' to be removed from manager")
	}

	// Old file should be deleted
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("expected old file %s to be deleted, err: %v", filePath, err)
	}

	// New trigger should exist
	renamedDef, exists := triggerMgr.Get("renamed-trigger")
	if !exists {
		t.Fatalf("expected 'renamed-trigger' to exist")
	}
	if renamedDef.Name != "Renamed Trigger" || renamedDef.Schedule != "*/5 * * * *" {
		t.Errorf("unexpected updated trigger def: %+v", renamedDef)
	}

	// 3. Test POST /ui/triggers (Create via Raw YAML)
	rawYAML := `id: raw-yaml-trigger
name: Raw Worker Trigger
type: worker
action:
  exec: node worker.js
  restart: always
  restart_delay: 2s
`
	rawForm := url.Values{}
	rawForm.Set("yaml", rawYAML)

	rawPostResp, err := client.PostForm(srv.URL+"/ui/triggers", rawForm)
	if err != nil {
		t.Fatalf("POST raw trigger failed: %v", err)
	}
	rawPostResp.Body.Close()
	if rawPostResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect (303), got %d", rawPostResp.StatusCode)
	}

	workerDef, exists := triggerMgr.Get("raw-yaml-trigger")
	if !exists {
		t.Fatalf("expected 'raw-yaml-trigger' to be created")
	}
	if workerDef.Type != trigger.TypeWorker || workerDef.Action.Restart != trigger.RestartAlways {
		t.Errorf("unexpected raw trigger def: %+v", workerDef)
	}

	// 4. Test POST /ui/triggers/renamed-trigger/delete
	delResp, err := client.PostForm(srv.URL+"/ui/triggers/renamed-trigger/delete", url.Values{})
	if err != nil {
		t.Fatalf("POST delete failed: %v", err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect (303), got %d", delResp.StatusCode)
	}

	if _, exists := triggerMgr.Get("renamed-trigger"); exists {
		t.Errorf("expected 'renamed-trigger' to be deleted")
	}

	// 5. Test DELETE /ui/triggers/raw-yaml-trigger
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/ui/triggers/raw-yaml-trigger", nil)
	delReqResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	delReqResp.Body.Close()
	if delReqResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect (303), got %d", delReqResp.StatusCode)
	}

	if _, exists := triggerMgr.Get("raw-yaml-trigger"); exists {
		t.Errorf("expected 'raw-yaml-trigger' to be deleted")
	}
}
