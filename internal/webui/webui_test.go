package webui

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/rs/zerolog"
)

type mockEvaluator struct {
	summaries []supervisor.AutomationSummary
}

func (m *mockEvaluator) AutomationsOverview(ctx context.Context) ([]supervisor.AutomationSummary, error) {
	return m.summaries, nil
}

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

	cfg := &config.Config{
		DataDir:  tempDir,
		LogLevel: "info",
	}

	logger := zerolog.New(io.Discard)
	reg := registry.New(logger)
	sup := supervisor.New(reg, nil, logger)
	approvals := supervisor.NewApprovalStore()
	eval := &mockEvaluator{
		summaries: []supervisor.AutomationSummary{
			{
				ActuatorID:  "test-actuator",
				Name:        "Test Actuator",
				Description: "A test actuator for webui tests",
				Triggers:    []string{"clara.test"},
				Routing:     "fast-path",
			},
		},
	}
	integ := &mockIntegLister{
		list: []map[string]any{
			{"name": "test-plugin", "status": "running"},
		},
	}

	ui := New(cfg, cfgPath, sup, reg, integ, eval, approvals, logger)

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
			name:         "Actuators List",
			path:         "/ui/actuators",
			expectedCode: http.StatusOK,
			contains:     "Test Actuator",
		},
		{
			name:         "Actuator Detail",
			path:         "/ui/actuators/test-actuator",
			expectedCode: http.StatusOK,
			contains:     "Test Actuator",
		},
		{
			name:         "Approvals List",
			path:         "/ui/approvals",
			expectedCode: http.StatusOK,
			contains:     "All Clear",
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
