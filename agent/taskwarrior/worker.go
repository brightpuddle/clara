// Package taskwarrior watches the taskwarrior data directory for changes and
// syncs pending tasks as ARTIFACT_KIND_TASK artifacts in the Clara database.
// Instead of polling on a timer, it uses fsnotify to detect file changes and
// only re-exports when the task data actually changes.
package taskwarrior

import (
"context"
"encoding/json"
"os"
"os/exec"
"path/filepath"
"strings"
"time"

"github.com/fsnotify/fsnotify"
"github.com/rs/zerolog"
"google.golang.org/protobuf/types/known/timestamppb"

artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
"github.com/brightpuddle/clara/internal/db"
)

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

// Worker watches the taskwarrior data directory and syncs tasks to the Clara DB.
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

// Run syncs on startup, then watches the taskwarrior data directory for
// changes, re-syncing whenever task data files are modified.
func (w *Worker) Run(ctx context.Context) {
// Initial sync on startup
w.sync(ctx)

// Find the taskwarrior data directory
dataDir := w.dataDir()
if dataDir == "" {
w.logger.Warn().Msg("could not determine taskwarrior data directory; file watching disabled")
<-ctx.Done()
return
}

watcher, err := fsnotify.NewWatcher()
if err != nil {
w.logger.Warn().Err(err).Msg("create fsnotify watcher")
<-ctx.Done()
return
}
defer watcher.Close()

if err := watcher.Add(dataDir); err != nil {
w.logger.Warn().Err(err).Str("dir", dataDir).Msg("watch taskwarrior data dir")
<-ctx.Done()
return
}

w.logger.Info().Str("dir", dataDir).Msg("watching taskwarrior data directory")

// Debounce: avoid re-syncing on every individual write during a bulk update
var debounce <-chan time.Time
for {
select {
case <-ctx.Done():
return
case ev, ok := <-watcher.Events:
if !ok {
return
}
// Only react to task data files (pending.data, completed.data, etc.)
base := filepath.Base(ev.Name)
if strings.HasSuffix(base, ".data") || strings.HasSuffix(base, ".json") {
debounce = time.After(250 * time.Millisecond)
}
case <-watcher.Errors:
// Log and continue
case <-debounce:
debounce = nil
w.sync(ctx)
}
}
}

// dataDir discovers the taskwarrior data directory by running `task _get rc.data.location`.
func (w *Worker) dataDir() string {
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
out, err := exec.CommandContext(ctx, w.binaryPath, "_get", "rc.data.location").Output()
if err == nil {
dir := strings.TrimSpace(string(out))
if dir != "" {
// Expand ~ if present
if strings.HasPrefix(dir, "~/") {
if home, err := os.UserHomeDir(); err == nil {
dir = filepath.Join(home, dir[2:])
}
}
if info, err := os.Stat(dir); err == nil && info.IsDir() {
return dir
}
}
}
// Fallback: ~/.task
if home, err := os.UserHomeDir(); err == nil {
fallback := filepath.Join(home, ".task")
if info, err := os.Stat(fallback); err == nil && info.IsDir() {
return fallback
}
}
return ""
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
