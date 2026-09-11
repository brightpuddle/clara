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

	tests := []struct {
		name         string
		path         string
		expectedCode int
		contains     string
	}{
		{
			name:         "Dashboard",
			path:         "/ui/",
			expectedCode: http.StatusOK,
			contains:     "Dashboard",
		},
		{
			name:         "Triggers List",
			path:         "/ui/triggers",
			expectedCode: http.StatusOK,
			contains:     "test-trigger",
		},
		{
			name:         "Trigger Detail",
			path:         "/ui/triggers/test-trigger",
			expectedCode: http.StatusOK,
			contains:     "test-trigger",
		},
		{
			name:         "Runs List",
			path:         "/ui/runs",
			expectedCode: http.StatusOK,
			contains:     "Execution Runs",
		},
		{
			name:         "Integrations",
			path:         "/ui/integrations",
			expectedCode: http.StatusOK,
			contains:     "test-plugin",
		},
		{
			name:         "Logs Page",
			path:         "/ui/logs",
			expectedCode: http.StatusOK,
			contains:     "Agent Observability",
		},
		{
			name:         "Configuration",
			path:         "/ui/config",
			expectedCode: http.StatusOK,
			contains:     "config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get(srv.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedCode {
				t.Errorf("expected status %d, got %d", tt.expectedCode, resp.StatusCode)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read response body: %v", err)
			}

			if !strings.Contains(string(body), tt.contains) {
				t.Errorf("expected body to contain %q, body:\n%s", tt.contains, string(body))
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

	// 1. Test GET /ui/config
	resp, err := client.Get(srv.URL + "/ui/config")
	if err != nil {
		t.Fatalf("GET /ui/config failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "Structured Settings") {
		t.Errorf("expected body to contain 'Structured Settings'")
	}
	if !strings.Contains(bodyStr, "github-mcp-server") {
		t.Errorf("expected body to contain initial mcp server config")
	}

	// 2. Test POST /ui/config with structured JSON (adding task_dirs, deleting plugin, adding mcp server)
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
			"discord": { "channel_id": "12345" }
		},
		"task_dirs": [
			"/tmp/tasks1",
			"/tmp/tasks3"
		],
		"plugins": [
			{ "name": "zk", "path": "" }
		],
		"plugin_search_paths": ["/opt/plugins"],
		"mcp_command_search_paths": ["/opt/bin"],
		"mcp_servers": [
			{
				"name": "eve",
				"server_type": "http",
				"url": "https://eve.example.com/mcp",
				"token": "token123",
				"skip_verify": true,
				"description": "Eve remote server"
			}
		],
		"integrations": {
			"db": { "path": "/tmp/new-db.sqlite" },
			"zk": { "vault_root": "/tmp/notes" }
		},
		"custom_integrations": [
			{
				"name": "llm",
				"yaml": "providers:\n  gemini:\n    model: gemini-2.0-flash"
			}
		]
	}`

	formData := url.Values{}
	formData.Set("config_json", newStructuredJSON)

	postResp, err := client.PostForm(srv.URL+"/ui/config", formData)
	if err != nil {
		t.Fatalf("POST /ui/config failed: %v", err)
	}
	postBody, _ := io.ReadAll(postResp.Body)
	postResp.Body.Close()

	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", postResp.StatusCode)
	}
	if !strings.Contains(string(postBody), "Configuration saved") {
		t.Errorf("expected success flash message, got: %s", string(postBody))
	}

	// 3. Verify the saved YAML on disk
	savedYAML, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read saved config: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(savedYAML, &parsed); err != nil {
		t.Fatalf("failed to unmarshal saved YAML: %v\nYAML:\n%s", err, string(savedYAML))
	}

	if parsed["log_level"] != "debug" {
		t.Errorf("expected log_level 'debug', got %v", parsed["log_level"])
	}
	if parsed["data_dir"] != "/tmp/custom-data" {
		t.Errorf("expected data_dir '/tmp/custom-data', got %v", parsed["data_dir"])
	}

	// Check task_dirs
	taskDirs, ok := parsed["task_dirs"].([]any)
	if !ok || len(taskDirs) != 2 || taskDirs[0] != "/tmp/tasks1" || taskDirs[1] != "/tmp/tasks3" {
		t.Errorf("expected task_dirs [/tmp/tasks1, /tmp/tasks3], got %v", parsed["task_dirs"])
	}

	// Check plugins
	plugins, ok := parsed["plugins"].([]any)
	if !ok || len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %v", parsed["plugins"])
	}
	pMap, _ := plugins[0].(map[string]any)
	if pMap["name"] != "zk" {
		t.Errorf("expected plugin name 'zk', got %v", pMap["name"])
	}

	// Check MCP server
	mcpServers, ok := parsed["mcp_servers"].([]any)
	if !ok || len(mcpServers) != 1 {
		t.Fatalf("expected 1 mcp server, got %v", parsed["mcp_servers"])
	}
	sMap, _ := mcpServers[0].(map[string]any)
	if sMap["name"] != "eve" || sMap["url"] != "https://eve.example.com/mcp" || sMap["skip_verify"] != true {
		t.Errorf("unexpected mcp server content: %v", sMap)
	}

	// Check integrations
	integs, ok := parsed["integrations"].(map[string]any)
	if !ok {
		t.Fatalf("expected integrations map, got %v", parsed["integrations"])
	}
	dbInteg, _ := integs["db"].(map[string]any)
	if dbInteg["path"] != "/tmp/new-db.sqlite" {
		t.Errorf("expected db path '/tmp/new-db.sqlite', got %v", dbInteg["path"])
	}
	llmInteg, _ := integs["llm"].(map[string]any)
	if llmInteg == nil {
		t.Errorf("expected custom integration 'llm' to be saved")
	}

	// 4. Test POST raw YAML
	rawFormData := url.Values{}
	rawFormData.Set("yaml", "log_level: warn\ndata_dir: /tmp/raw-dir\n")
	rawResp, err := client.PostForm(srv.URL+"/ui/config", rawFormData)
	if err != nil {
		t.Fatalf("POST raw YAML failed: %v", err)
	}
	rawResp.Body.Close()

	savedRaw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(savedRaw), "log_level: warn") {
		t.Errorf("expected raw YAML to be saved, got: %s", string(savedRaw))
	}
}

func TestWebUI_TriggerCRUD(t *testing.T) {
	tempDir := t.TempDir()
	triggersDir := filepath.Join(tempDir, "triggers")
	if err := os.MkdirAll(triggersDir, 0o755); err != nil {
		t.Fatalf("failed to create triggers dir: %v", err)
	}

	cfgPath := filepath.Join(tempDir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte("log_level: info\n"), 0o644)

	logger := zerolog.New(io.Discard)
	dbPath := filepath.Join(tempDir, "clara.db")
	db, err := store.Open(dbPath, logger)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	cfg := &config.Config{
		DataDir:             tempDir,
		LogLevel:            "info",
		TriggerDirsOverride: []string{triggersDir},
	}

	reg := registry.New(logger)
	sup := supervisor.New(reg, nil, logger)
	eventBus := supervisor.NewEventBus()
	triggerMgr := trigger.NewManager(nil, eventBus, db)
	integ := &mockIntegLister{}

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

	// 1. Test GET /ui/triggers/new
	resp, err := client.Get(srv.URL + "/ui/triggers/new")
	if err != nil {
		t.Fatalf("GET /ui/triggers/new failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "New Trigger") {
		t.Errorf("expected body to contain 'New Trigger'")
	}

	// 2. Test POST /ui/triggers (Structured JSON)
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

	// 3. Test GET /ui/triggers/my-email-trigger/edit
	editResp, err := client.Get(srv.URL + "/ui/triggers/my-email-trigger/edit")
	if err != nil {
		t.Fatalf("GET /ui/triggers/my-email-trigger/edit failed: %v", err)
	}
	editBody, _ := io.ReadAll(editResp.Body)
	editResp.Body.Close()
	if editResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", editResp.StatusCode)
	}
	if !strings.Contains(string(editBody), "Edit Trigger") || !strings.Contains(string(editBody), "my-email-trigger") {
		t.Errorf("expected body to contain 'Edit Trigger' and 'my-email-trigger'")
	}

	// 4. Test POST /ui/triggers/my-email-trigger (Update and rename ID)
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

	// 5. Test POST /ui/triggers (Create via Raw YAML)
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

	// 6. Test POST /ui/triggers/renamed-trigger/delete
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

	// 7. Test DELETE /ui/triggers/raw-yaml-trigger
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
