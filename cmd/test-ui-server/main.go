package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/webui"
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

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3333"
	}

	tempDir, err := os.MkdirTemp("", "clara-webui-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDir)

	cfgPath := filepath.Join(tempDir, "config.yaml")
	sampleYAML := `log_level: info
data_dir: ` + tempDir + `
task_dirs:
  - /tmp/tasks1
plugins:
  - name: chrome
mcp_servers:
  - name: github
    command: github-mcp-server stdio
    description: GitHub tools
integrations:
  db:
    path: /tmp/db.sqlite
`
	if err := os.WriteFile(cfgPath, []byte(sampleYAML), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write test config: %v\n", err)
		os.Exit(1)
	}

	logPath := filepath.Join(tempDir, "clara.log")
	logContent := `{"level":"info","time":"` + time.Now().Format(time.RFC3339) + `","message":"clara test daemon initialized"}
{"level":"debug","time":"` + time.Now().Format(time.RFC3339) + `","message":"evaluator ready"}
{"level":"info","time":"` + time.Now().Format(time.RFC3339) + `","message":"integration plugins loaded"}
`
	_ = os.WriteFile(logPath, []byte(logContent), 0o644)

	cfg := &config.Config{
		DataDir:  tempDir,
		LogLevel: "info",
	}

	logger := zerolog.New(io.Discard)
	reg := registry.New(logger)
	sup := supervisor.New(reg, nil, logger)
	approvals := supervisor.NewApprovalStore()

	// Add sample pending approvals in background goroutine so Submit keeps it pending
	go approvals.Submit(context.Background(), supervisor.ApprovalRequest{
		RequestID: "req-test-1",
		Context:   "Actuator requests capability: write access to ~/.ssh/config",
		Options: []supervisor.ResolutionOption{
			{ID: "allow", Description: "Allow once", ActionCode: "allow"},
			{ID: "deny", Description: "Deny and block", ActionCode: "deny"},
		},
	})

	eval := &mockEvaluator{
		summaries: []supervisor.AutomationSummary{
			{
				ActuatorID:  "github-pr-reviewer",
				Name:        "GitHub PR Reviewer",
				Description: "Automatically reviews incoming pull requests and comments feedback",
				Triggers:    []string{"github.pull_request.opened", "github.pull_request.synchronize"},
				Routing:     "fast-path",
				RuleID:      "rule-gh-pr-123",
				ExpiresIn:   "24h",
			},
			{
				ActuatorID:  "slack-incident-responder",
				Name:        "Slack Incident Responder",
				Description: "Coordinates incident response triage and notifications",
				Triggers:    []string{"incident.alert"},
				Routing:     "llm-dynamic",
			},
		},
	}

	integ := &mockIntegLister{
		list: []map[string]any{
			{"name": "chrome", "status": "running", "description": "Headless Chrome bridge"},
			{"name": "discord", "status": "running", "description": "Discord message sensor"},
		},
	}

	ui := webui.New(cfg, cfgPath, sup, reg, integ, eval, approvals, logger)
	mux := http.NewServeMux()
	ui.Mount(mux)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		fmt.Printf("Test UI server listening on http://127.0.0.1:%s\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
