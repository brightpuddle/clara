// Package taskwarrior polls taskwarrior for pending tasks and syncs them
// as ARTIFACT_KIND_TASK artifacts in the Clara database.
package taskwarrior

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/timestamppb"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	"github.com/brightpuddle/clara/internal/db"
)

const pollInterval = 30 * time.Second

// Task is the JSON structure returned by `task export`.
type Task struct {
	UUID        string   `json:"uuid"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Project     string   `json:"project,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Due         string   `json:"due,omitempty"`
	Entry       string   `json:"entry,omitempty"`
	Modified    string   `json:"modified,omitempty"`
	Urgency     float64  `json:"urgency,omitempty"`
	Annotations []struct {
		Description string `json:"description"`
	} `json:"annotations,omitempty"`
}

// Worker polls taskwarrior and syncs tasks to the Clara DB.
type Worker struct {
	binaryPath string
	db         *db.DB
	notifyCh   chan *artifactv1.Artifact
	logger     zerolog.Logger
}

// New creates a new taskwarrior Worker.
func New(binaryPath string, database *db.DB, logger zerolog.Logger) *Worker {
	if binaryPath == "" {
		binaryPath = "task"
	}
	return &Worker{
		binaryPath: binaryPath,
		db:         database,
		notifyCh:   make(chan *artifactv1.Artifact, 64),
		logger:     logger,
	}
}

// Notifications returns the channel on which artifact updates are sent.
func (w *Worker) Notifications() <-chan *artifactv1.Artifact {
	return w.notifyCh
}

// Run starts the poll loop and blocks until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	w.sync(ctx)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.sync(ctx)
		}
	}
}

func (w *Worker) sync(ctx context.Context) {
	out, err := exec.CommandContext(ctx, w.binaryPath, "rc.confirmation=no", "status:pending", "export").Output()
	if err != nil {
		w.logger.Warn().Err(err).Msg("task export failed")
		return
	}

	var tasks []Task
	if err := json.Unmarshal(out, &tasks); err != nil {
		w.logger.Warn().Err(err).Msg("parse task export output")
		return
	}

	newIDs := make(map[string]bool)
	for _, t := range tasks {
		newIDs["task:"+t.UUID] = true
	}

	existing, err := w.db.ListArtifacts(ctx, 0, 0, []artifactv1.ArtifactKind{artifactv1.ArtifactKind_ARTIFACT_KIND_TASK})
	if err == nil {
		for _, e := range existing {
			if !newIDs[e.Id] {
				if err := w.db.MarkDone(ctx, e.Id); err == nil {
					select {
					case w.notifyCh <- e:
					default:
					}
				}
			}
		}
	}

	for _, t := range tasks {
		a := taskToArtifact(t)
		if err := w.db.UpsertArtifact(ctx, a); err != nil {
			w.logger.Warn().Err(err).Str("uuid", t.UUID).Msg("upsert task artifact")
			continue
		}
		select {
		case w.notifyCh <- a:
		default:
		}
	}
	w.logger.Debug().Int("count", len(tasks)).Msg("synced taskwarrior tasks")
}

// MarkDone calls `task UUID done` to complete a task in taskwarrior.
func (w *Worker) MarkDone(ctx context.Context, uuid string) error {
	cmd := exec.CommandContext(ctx, w.binaryPath, uuid, "done")
	return cmd.Run()
}

func taskToArtifact(t Task) *artifactv1.Artifact {
	id := "task:" + t.UUID

	meta := map[string]string{}
	if t.Project != "" {
		meta["project"] = t.Project
	}
	if t.Priority != "" {
		meta["priority"] = t.Priority
	}

	var content string
	for _, ann := range t.Annotations {
		content += ann.Description + "\n"
	}

	a := &artifactv1.Artifact{
		Id:         id,
		Kind:       artifactv1.ArtifactKind_ARTIFACT_KIND_TASK,
		Title:      t.Description,
		Content:    content,
		SourcePath: t.UUID,
		SourceApp:  "taskwarrior",
		Tags:       t.Tags,
		Metadata:   meta,
		HeatScore:  t.Urgency / 20.0,
	}

	if ts := parseTaskTime(t.Entry); ts != nil {
		a.CreatedAt = timestamppb.New(*ts)
	}
	if ts := parseTaskTime(t.Modified); ts != nil {
		a.UpdatedAt = timestamppb.New(*ts)
	}
	if ts := parseTaskTime(t.Due); ts != nil {
		a.DueAt = timestamppb.New(*ts)
	}

	if a.CreatedAt == nil {
		a.CreatedAt = timestamppb.Now()
	}
	if a.UpdatedAt == nil {
		a.UpdatedAt = timestamppb.Now()
	}
	return a
}

// parseTaskTime parses taskwarrior's date format: 20260101T120000Z
func parseTaskTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("20060102T150405Z", s)
	if err != nil {
		return nil
	}
	return &t
}
