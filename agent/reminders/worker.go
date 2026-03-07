// Package reminders polls the Swift native worker for Apple Reminders
// and ingests them as artifacts in the local database.
package reminders

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	nativev1 "github.com/brightpuddle/clara/gen/native/v1"
	"github.com/brightpuddle/clara/internal/artifact"
	"github.com/brightpuddle/clara/internal/db"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const pollInterval = 60 * time.Second

// Worker polls the native worker for reminders and syncs them to the DB.
type Worker struct {
	native   nativev1.NativeWorkerServiceClient
	db       *db.DB
	notifyCh chan *artifactv1.Artifact
	logger   zerolog.Logger
}

// New creates a new reminders Worker.
func New(native nativev1.NativeWorkerServiceClient, database *db.DB, logger zerolog.Logger) *Worker {
	return &Worker{
		native:   native,
		db:       database,
		notifyCh: make(chan *artifactv1.Artifact, 64),
		logger:   logger,
	}
}

// Notifications returns a channel that emits newly synced reminder artifacts.
func (w *Worker) Notifications() <-chan *artifactv1.Artifact {
	return w.notifyCh
}

// Run begins polling at regular intervals until ctx is cancelled.
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
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := w.native.ListReminders(callCtx, &nativev1.ListRemindersRequest{
		IncludeCompleted: false,
	})
	if err != nil {
		w.logger.Warn().Err(err).Msg("list reminders from native worker")
		return
	}

	for _, r := range resp.Reminders {
		a := reminderToArtifact(r)
		if err := w.db.UpsertArtifact(ctx, a); err != nil {
			w.logger.Warn().Err(err).Str("id", r.Id).Msg("upsert reminder artifact")
			continue
		}
		select {
		case w.notifyCh <- a:
		default:
		}
	}
	w.logger.Debug().Int("count", len(resp.Reminders)).Msg("synced reminders")
}

func reminderToArtifact(r *nativev1.Reminder) *artifactv1.Artifact {
	a := &artifactv1.Artifact{
		Id:         "reminder:" + r.Id,
		Kind:       artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER,
		Title:      r.Title,
		Content:    r.Notes,
		SourcePath: r.Id, // native EventKit identifier
		SourceApp:  "reminders",
		Tags:       []string{},
		Metadata:   map[string]string{"list": r.ListName},
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.ModifiedAt,
		DueAt:      r.DueDate,
	}
	if a.CreatedAt == nil {
		a.CreatedAt = timestamppb.New(time.Now())
	}
	if a.UpdatedAt == nil {
		a.UpdatedAt = timestamppb.New(time.Now())
	}
	a.HeatScore = artifact.ComputeHeatScore(a)
	return a
}
