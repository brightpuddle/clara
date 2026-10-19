// Package clara provides built-in tools for inspecting Clara's execution runs,
// trigger configurations, audit logs, and staging workflows.
package clara

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
)

const description = "Built-in Clara system inspection: triggers, execution run audits, and workflow staging."

// Service provides tool handlers for Clara meta-operations.
type Service struct {
	cfg        *config.Config
	db         *store.Store
	triggerMgr *trigger.Manager
	log        zerolog.Logger
}

// Register adds all clara tools into reg under the "clara" namespace.
func Register(
	_ context.Context,
	cfg *config.Config,
	db *store.Store,
	triggerMgr *trigger.Manager,
	reg *registry.Registry,
	log zerolog.Logger,
) error {
	log.Debug().Msg("registering clara builtin")

	svc := &Service{
		cfg:        cfg,
		db:         db,
		triggerMgr: triggerMgr,
		log:        log.With().Str("component", "builtin_clara").Logger(),
	}

	reg.RegisterNamespaceDescription("clara", description)

	tools := []struct {
		spec    mcp.Tool
		handler func(context.Context, map[string]any) (any, error)
	}{
		{
			spec: mcp.NewTool("clara.runs_list",
				mcp.WithDescription("List recent Clara trigger execution runs and their status, exit codes, and durations."),
				mcp.WithNumber("limit", mcp.Description("Max runs to retrieve (default: 20)")),
				mcp.WithString("trigger_id", mcp.Description("Optional filter by trigger ID")),
			),
			handler: svc.handleRunsList,
		},
		{
			spec: mcp.NewTool("clara.run_get",
				mcp.WithDescription("Get detailed execution log (stdout, stderr, exit code, event data) for a specific trigger run ID."),
				mcp.WithString("id", mcp.Required(), mcp.Description("The execution run ID")),
			),
			handler: svc.handleRunGet,
		},
		{
			spec: mcp.NewTool("clara.triggers_list",
				mcp.WithDescription("List all active Clara triggers (event rules, schedules, workers)."),
			),
			handler: svc.handleTriggersList,
		},
		{
			spec: mcp.NewTool("clara.trigger_run",
				mcp.WithDescription("Manually invoke and supervise a Clara trigger by ID."),
				mcp.WithString("id", mcp.Required(), mcp.Description("The trigger ID to run")),
				mcp.WithObject("event", mcp.Description("Optional mock CloudEvent payload to pass to the trigger action")),
			),
			handler: svc.handleTriggerRun,
		},
		{
			spec: mcp.NewTool("clara.staging_list",
				mcp.WithDescription("List candidate/draft triggers and automation scripts staged in ~/.config/clara/triggers-available/ (or tasks/staging/)."),
			),
			handler: svc.handleStagingList,
		},
		{
			spec: mcp.NewTool("clara.staging_save",
				mcp.WithDescription("Save a new candidate trigger YAML definition and optional TypeScript/script into the Clara staging directory."),
				mcp.WithString("filename", mcp.Required(), mcp.Description("Filename for the trigger (e.g. 'auto_pr_review.yaml')")),
				mcp.WithString("yaml_content", mcp.Required(), mcp.Description("YAML content of the trigger definition")),
				mcp.WithString("script_path", mcp.Description("Optional relative script path (e.g. 'scripts/auto_pr_review.ts')")),
				mcp.WithString("script_content", mcp.Description("Optional content for the script")),
			),
			handler: svc.handleStagingSave,
		},
		{
			spec: mcp.NewTool("clara.staging_promote",
				mcp.WithDescription("Promote a staged trigger into active ~/.config/clara/triggers/ (symlinks into active directory)."),
				mcp.WithString("filename", mcp.Required(), mcp.Description("Filename in triggers-available to activate (e.g. 'apple_mail_triage.yaml')")),
			),
			handler: svc.handleStagingPromote,
		},
		{
			spec: mcp.NewTool("clara.activity_history",
				mcp.WithDescription("Query recent macOS user activity samples (active application, window titles, idle seconds) collected by Clara's ambient sensor."),
				mcp.WithNumber("limit", mcp.Description("Max samples to return (default: 50)")),
				mcp.WithNumber("since_minutes", mcp.Description("Retrieve samples within the last N minutes (default: 60)")),
			),
			handler: svc.handleActivityHistory,
		},
	}

	for _, t := range tools {
		reg.RegisterWithSpec(t.spec, t.handler)
	}

	return nil
}

func (s *Service) handleRunsList(ctx context.Context, args map[string]any) (any, error) {
	if s.db == nil {
		return nil, errors.New("audit database unavailable")
	}
	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	triggerID, _ := args["trigger_id"].(string)

	runs, err := s.db.ListTriggerRuns(ctx, limit, triggerID)
	if err != nil {
		return nil, errors.Wrap(err, "list trigger runs")
	}
	return runs, nil
}

func (s *Service) handleRunGet(ctx context.Context, args map[string]any) (any, error) {
	if s.db == nil {
		return nil, errors.New("audit database unavailable")
	}
	id, _ := args["id"].(string)
	if id == "" {
		return nil, errors.New("id is required")
	}
	run, err := s.db.GetTriggerRun(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, "get trigger run")
	}
	if run == nil {
		return nil, errors.Newf("run %q not found", id)
	}
	return run, nil
}

func (s *Service) handleTriggersList(_ context.Context, _ map[string]any) (any, error) {
	if s.triggerMgr == nil {
		return nil, errors.New("trigger manager unavailable")
	}
	return s.triggerMgr.List(), nil
}

func (s *Service) handleTriggerRun(ctx context.Context, args map[string]any) (any, error) {
	if s.triggerMgr == nil {
		return nil, errors.New("trigger manager unavailable")
	}
	id, _ := args["id"].(string)
	if id == "" {
		return nil, errors.New("id is required")
	}
	record, err := s.triggerMgr.Run(ctx, id, args["event"])
	if err != nil {
		return nil, errors.Wrap(err, "run trigger")
	}
	return record, nil
}

func (s *Service) stagingDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clara", "triggers-available")
}

func (s *Service) activeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clara", "triggers")
}

func (s *Service) configRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clara")
}

type StagedItem struct {
	Filename   string `json:"filename"`
	Path       string `json:"path"`
	IsActive   bool   `json:"is_active"`
	Triggers   any    `json:"triggers,omitempty"`
	RawContent string `json:"raw_content,omitempty"`
}

func (s *Service) handleStagingList(_ context.Context, _ map[string]any) (any, error) {
	dir := s.stagingDir()
	_ = os.MkdirAll(dir, 0o755)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.Wrap(err, "read staging dir")
	}

	activeDir := s.activeDir()
	_ = os.MkdirAll(activeDir, 0o755)

	var items []StagedItem
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml")) {
			continue
		}
		stagedPath := filepath.Join(dir, entry.Name())
		contentBytes, err := os.ReadFile(stagedPath)
		if err != nil {
			continue
		}

		activePath := filepath.Join(activeDir, entry.Name())
		_, activeErr := os.Lstat(activePath)
		isActive := activeErr == nil

		var parsed map[string]any
		_ = yaml.Unmarshal(contentBytes, &parsed)

		items = append(items, StagedItem{
			Filename:   entry.Name(),
			Path:       stagedPath,
			IsActive:   isActive,
			Triggers:   parsed["triggers"],
			RawContent: string(contentBytes),
		})
	}

	return items, nil
}

func (s *Service) handleStagingSave(_ context.Context, args map[string]any) (any, error) {
	filename, _ := args["filename"].(string)
	yamlContent, _ := args["yaml_content"].(string)
	if filename == "" || yamlContent == "" {
		return nil, errors.New("filename and yaml_content are required")
	}

	if !strings.HasSuffix(filename, ".yaml") && !strings.HasSuffix(filename, ".yml") {
		filename += ".yaml"
	}

	// Validate YAML syntax
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &parsed); err != nil {
		return nil, errors.Wrap(err, "invalid trigger YAML format")
	}

	stagedDir := s.stagingDir()
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		return nil, errors.Wrap(err, "create staging dir")
	}

	targetPath := filepath.Join(stagedDir, filepath.Base(filename))
	if err := os.WriteFile(targetPath, []byte(yamlContent), 0o644); err != nil {
		return nil, errors.Wrap(err, "write trigger file")
	}

	// If a supporting script was provided, write it relative to ~/.config/clara/
	scriptPath, _ := args["script_path"].(string)
	scriptContent, _ := args["script_content"].(string)
	if scriptPath != "" && scriptContent != "" {
		fullScriptPath := filepath.Join(s.configRoot(), scriptPath)
		if err := os.MkdirAll(filepath.Dir(fullScriptPath), 0o755); err != nil {
			return nil, errors.Wrap(err, "create script directory")
		}
		if err := os.WriteFile(fullScriptPath, []byte(scriptContent), 0o755); err != nil {
			return nil, errors.Wrap(err, "write script file")
		}
	}

	return map[string]any{
		"saved_trigger": targetPath,
		"status":        "staged",
		"message":       fmt.Sprintf("Saved trigger to %s. Use clara.staging_promote to activate.", targetPath),
	}, nil
}

func (s *Service) handleStagingPromote(_ context.Context, args map[string]any) (any, error) {
	filename, _ := args["filename"].(string)
	if filename == "" {
		return nil, errors.New("filename is required")
	}
	filename = filepath.Base(filename)
	stagedPath := filepath.Join(s.stagingDir(), filename)
	if _, err := os.Stat(stagedPath); err != nil {
		return nil, errors.Wrapf(err, "staged trigger %q not found", filename)
	}

	activeDir := s.activeDir()
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		return nil, errors.Wrap(err, "create triggers dir")
	}

	activePath := filepath.Join(activeDir, filename)
	// Remove existing symlink or file if present
	_ = os.Remove(activePath)

	relTarget := filepath.Join("..", "triggers-available", filename)
	if err := os.Symlink(relTarget, activePath); err != nil {
		return nil, errors.Wrap(err, "symlink trigger into active triggers directory")
	}

	return map[string]any{
		"active_path": activePath,
		"status":      "activated",
		"message":     fmt.Sprintf("Promoted %s to active triggers directory", filename),
	}, nil
}

func (s *Service) handleActivityHistory(ctx context.Context, args map[string]any) (any, error) {
	if s.db == nil {
		return nil, errors.New("audit database unavailable")
	}
	limit := 50
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	sinceMinutes := 60.0
	if m, ok := args["since_minutes"].(float64); ok && m > 0 {
		sinceMinutes = m
	}
	sinceTime := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute)

	samples, err := s.db.ListActivitySamples(ctx, limit, sinceTime)
	if err != nil {
		return nil, errors.Wrap(err, "list activity samples")
	}
	return samples, nil
}
