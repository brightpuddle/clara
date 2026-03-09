package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	agentclient "github.com/brightpuddle/clara/app/agent"
	agentv1 "github.com/brightpuddle/clara/gen/agent/v1"
	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	claraconfig "github.com/brightpuddle/clara/internal/config"
)

// Artifact is the frontend-facing artifact struct (JSON-serialised by Wails).
type Artifact struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	SourcePath string   `json:"source_path"`
	SourceApp  string   `json:"source_app"`
	HeatScore  float32  `json:"heat_score"`
	Tags       []string `json:"tags"`
	DueAt      *int64   `json:"due_at,omitempty"` // unix seconds, nil if not set
}

// ArtifactDetail wraps an artifact with its related items.
type ArtifactDetail struct {
	Artifact Artifact   `json:"artifact"`
	Related  []Artifact `json:"related"`
}

// ComponentStatus mirrors agentv1.ComponentStatus for the frontend.
type ComponentStatus struct {
	Connected     bool   `json:"connected"`
	State         string `json:"state"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	Fault         string `json:"fault,omitempty"`
}

// Status is the frontend-facing status struct.
type Status struct {
	Agent          ComponentStatus `json:"agent"`
	Native         ComponentStatus `json:"native"`
	ArtifactCounts map[string]int32 `json:"artifact_counts"`
}

// App holds the Wails application state.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	client *agentclient.Client
	mu     sync.RWMutex

	socketPath string
}

// NewApp creates a new App.
func NewApp() *App {
	home, _ := filepath.Abs(".")
	_ = home
	// Build the default socket path (~/.local/share/clara/agent.sock).
	return &App{
		socketPath: defaultSocketPath(),
	}
}

// startup is called by Wails when the application starts.
func (a *App) startup(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)
	// Validate config and emit an error event if invalid
	cfg, err := claraconfig.Load()
	if err != nil {
		runtime.EventsEmit(ctx, "config:error", err.Error())
	} else if err := cfg.Validate(); err != nil {
		runtime.EventsEmit(ctx, "config:error", err.Error())
	}
	go a.connectLoop()
}

// shutdown is called by Wails when the application shuts down.
func (a *App) shutdown(_ context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Lock()
	if a.client != nil {
		_ = a.client.Close()
	}
	a.mu.Unlock()
}

// connectLoop establishes the agent connection and starts the subscribe loop,
// retrying with exponential backoff on failure.
func (a *App) connectLoop() {
	backoff := time.Second
	for {
		select {
		case <-a.ctx.Done():
			return
		default:
		}

		client, err := agentclient.New(a.socketPath)
		if err != nil {
			runtime.EventsEmit(a.ctx, "agent:disconnected", err.Error())
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(backoff):
				if backoff < 30*time.Second {
					backoff *= 2
				}
				continue
			}
		}

		backoff = time.Second
		a.mu.Lock()
		a.client = client
		a.mu.Unlock()

		runtime.EventsEmit(a.ctx, "agent:connected", nil)

		// Emit current theme on connect and start watching for changes
		go a.watchTheme(client)

		a.runSubscribeLoop(client)

		a.mu.Lock()
		a.client = nil
		a.mu.Unlock()
	}
}

// watchTheme polls the system theme every 5 seconds and emits theme:changed on changes.
func (a *App) watchTheme(client *agentclient.Client) {
	var lastDark *bool
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	// Emit immediately on connect
	check := func() {
		ctx, cancel := context.WithTimeout(a.ctx, 3*time.Second)
		defer cancel()
		dark, err := client.GetSystemTheme(ctx)
		if err != nil {
			return
		}
		if lastDark == nil || *lastDark != dark {
			lastDark = &dark
			runtime.EventsEmit(a.ctx, "theme:changed", dark)
		}
	}
	check()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

// runSubscribeLoop listens for artifact events and emits Wails events.
func (a *App) runSubscribeLoop(client *agentclient.Client) {
	ch, err := client.Subscribe(a.ctx)
	if err != nil {
		return
	}
	for ev := range ch {
		switch ev.Type {
		case agentv1.EventType_EVENT_TYPE_CREATED:
			runtime.EventsEmit(a.ctx, "artifact:created", protoToArtifact(ev.Artifact))
		case agentv1.EventType_EVENT_TYPE_UPDATED:
			runtime.EventsEmit(a.ctx, "artifact:updated", protoToArtifact(ev.Artifact))
		case agentv1.EventType_EVENT_TYPE_DELETED:
			runtime.EventsEmit(a.ctx, "artifact:deleted", map[string]string{"id": ev.Artifact.GetId()})
		}
	}
}

// client returns the current gRPC client or an error if not connected.
func (a *App) getClient() (*agentclient.Client, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.client == nil {
		return nil, fmt.Errorf("not connected to agent")
	}
	return a.client, nil
}

// ---- Wails bound methods (callable from frontend) ----

// ListArtifacts returns all non-done artifacts, optionally filtered by kind.
func (a *App) ListArtifacts(kinds []string) ([]Artifact, error) {
	c, err := a.getClient()
	if err != nil {
		return nil, err
	}
	protoKinds := stringsToKinds(kinds)
	artifacts, err := c.ListArtifacts(a.ctx, protoKinds)
	if err != nil {
		return nil, err
	}
	result := make([]Artifact, len(artifacts))
	for i, a := range artifacts {
		result[i] = protoToArtifact(a)
	}
	return result, nil
}

// GetArtifact returns an artifact and its related neighbors.
func (a *App) GetArtifact(id string) (ArtifactDetail, error) {
	c, err := a.getClient()
	if err != nil {
		return ArtifactDetail{}, err
	}
	artifact, related, err := c.GetArtifact(a.ctx, id)
	if err != nil {
		return ArtifactDetail{}, err
	}
	detail := ArtifactDetail{Artifact: protoToArtifact(artifact)}
	detail.Related = make([]Artifact, len(related))
	for i, r := range related {
		detail.Related[i] = protoToArtifact(r)
	}
	return detail, nil
}

// MarkDone marks an artifact as done and removes it from the list.
func (a *App) MarkDone(id string) error {
	c, err := a.getClient()
	if err != nil {
		return err
	}
	return c.MarkDone(a.ctx, id)
}

// Search performs a text search across all artifacts.
func (a *App) Search(query string, limit int32) ([]Artifact, error) {
	c, err := a.getClient()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	artifacts, err := c.Search(a.ctx, query, limit)
	if err != nil {
		return nil, err
	}
	result := make([]Artifact, len(artifacts))
	for i, a := range artifacts {
		result[i] = protoToArtifact(a)
	}
	return result, nil
}

// GetStatus returns live status for all Clara components.
func (a *App) GetStatus() (Status, error) {
	c, err := a.getClient()
	if err != nil {
		return Status{}, err
	}
	resp, err := c.GetStatus(a.ctx)
	if err != nil {
		return Status{}, err
	}
	s := Status{ArtifactCounts: resp.ArtifactCounts}
	if resp.Agent != nil {
		s.Agent = protoToComponentStatus(resp.Agent)
	}
	if resp.Native != nil {
		s.Native = protoToComponentStatus(resp.Native)
	}
	return s, nil
}

// OpenNative opens an artifact in its native app (e.g. Reminders.app).
func (a *App) OpenNative(art Artifact) error {
	switch art.SourceApp {
	case "reminders":
		if art.SourcePath != "" {
			script := fmt.Sprintf(`tell application "Reminders"
	activate
	repeat with theList in every list
		set matches to (reminders of theList whose id is "%s")
		if (count of matches) > 0 then
			show item 1 of matches
			exit repeat
		end if
	end repeat
end tell`, art.SourcePath)
			return exec.Command("/usr/bin/osascript", "-e", script).Run()
		}
		return exec.Command("/usr/bin/open", "-a", "Reminders").Run()
	default:
		if art.SourcePath != "" {
			return exec.Command("/usr/bin/open", art.SourcePath).Run()
		}
	}
	return nil
}

// ShowWindow brings the main window to the foreground.
func (a *App) ShowWindow() {
	runtime.WindowShow(a.ctx)
}

// ---- Helpers ----

func protoToArtifact(a *artifactv1.Artifact) Artifact {
	if a == nil {
		return Artifact{}
	}
	art := Artifact{
		ID:         a.Id,
		Kind:       kindToString(a.Kind),
		Title:      a.Title,
		Content:    a.Content,
		SourcePath: a.SourcePath,
		SourceApp:  a.SourceApp,
		HeatScore:  float32(a.HeatScore),
		Tags:       a.Tags,
	}
	if a.DueAt != nil {
		ts := a.DueAt.AsTime().Unix()
		art.DueAt = &ts
	}
	return art
}

func protoToComponentStatus(cs *agentv1.ComponentStatus) ComponentStatus {
	return ComponentStatus{
		Connected:     cs.Connected,
		State:         cs.State,
		UptimeSeconds: cs.UptimeSeconds,
		Fault:         cs.Fault,
	}
}

func kindToString(k artifactv1.ArtifactKind) string {
	return strings.ToLower(strings.TrimPrefix(k.String(), "ARTIFACT_KIND_"))
}

func stringsToKinds(kinds []string) []artifactv1.ArtifactKind {
	if len(kinds) == 0 {
		return nil
	}
	result := make([]artifactv1.ArtifactKind, 0, len(kinds))
	for _, k := range kinds {
		upper := "ARTIFACT_KIND_" + strings.ToUpper(k)
		if v, ok := artifactv1.ArtifactKind_value[upper]; ok {
			result = append(result, artifactv1.ArtifactKind(v))
		}
	}
	return result
}

func defaultSocketPath() string {
	return filepath.Join(homeDir(), ".local", "share", "clara", "agent.sock")
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "/tmp"
}

