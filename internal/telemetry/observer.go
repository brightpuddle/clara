// Package telemetry provides background ambient observation of user activity on macOS.
// It samples the active frontmost application, window title, and user idle duration,
// publishing CloudEvents to the event bus and persisting history to the audit store.
package telemetry

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/supervisor"
)

// Observer continuously polls macOS frontmost app and user idle duration.
type Observer struct {
	bus      *supervisor.EventBus
	db       *store.Store
	interval time.Duration
	log      zerolog.Logger
	mu       sync.Mutex
	lastApp  string
	lastWin  string
}

// NewObserver constructs a new Observer instance.
func NewObserver(bus *supervisor.EventBus, db *store.Store, interval time.Duration, log zerolog.Logger) *Observer {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Observer{
		bus:      bus,
		db:       db,
		interval: interval,
		log:      log.With().Str("component", "telemetry").Logger(),
	}
}

// Start runs the observation loop until ctx is cancelled.
func (o *Observer) Start(ctx context.Context) {
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()

	o.log.Info().Dur("interval", o.interval).Msg("starting ambient activity observer")

	for {
		select {
		case <-ctx.Done():
			o.log.Info().Msg("stopping ambient activity observer")
			return
		case <-ticker.C:
			o.sample(ctx)
		}
	}
}

func (o *Observer) sample(ctx context.Context) {
	idleSecs := getMacIdleSeconds(ctx)

	// If the user has been idle for more than 5 minutes, mark as idle and reduce polling noise
	if idleSecs > 300 {
		o.mu.Lock()
		changed := o.lastApp != "Idle"
		o.lastApp = "Idle"
		o.lastWin = ""
		o.mu.Unlock()

		if changed {
			o.publishSample(ctx, "Idle", "", idleSecs)
		}
		return
	}

	appName, winTitle := getFrontmostAppAndTitle(ctx)
	if appName == "" {
		return
	}

	o.mu.Lock()
	appChanged := appName != o.lastApp || winTitle != o.lastWin
	o.lastApp = appName
	o.lastWin = winTitle
	o.mu.Unlock()

	// Record sample if app/window changed or periodically
	if appChanged {
		o.publishSample(ctx, appName, winTitle, idleSecs)
	}
}

func (o *Observer) publishSample(ctx context.Context, appName, winTitle string, idleSecs int64) {
	sampleID := fmt.Sprintf("act-%d", time.Now().UnixNano())
	now := time.Now()

	// 1. Persist to audit store if available
	if o.db != nil {
		_ = o.db.RecordActivitySample(ctx, store.ActivitySampleRecord{
			ID:          sampleID,
			AppName:     appName,
			WindowTitle: winTitle,
			IdleSecs:    idleSecs,
			Metadata:    "{}",
			SampledAt:   now,
		})
	}

	// 2. Publish CloudEvent onto EventBus
	if o.bus != nil {
		o.bus.PublishCloud(supervisor.CloudEvent{
			ID:          sampleID,
			Source:      "sensor.macos.activity",
			Type:        "telemetry.activity.sample",
			Time:        now,
			ContentType: "application/json",
			Data: map[string]any{
				"app_name":     appName,
				"window_title": winTitle,
				"idle_secs":    idleSecs,
			},
		})
	}
}

// getFrontmostAppAndTitle queries the active application and window title using osascript.
func getFrontmostAppAndTitle(ctx context.Context) (string, string) {
	script := `
		tell application "System Events"
			set frontApp to first application process whose frontmost is true
			set appName to name of frontApp
			set winTitle to ""
			try
				set winTitle to name of front window of frontApp
			end try
			return appName & ":::" & winTitle
		end tell
	`
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}

	parts := strings.Split(strings.TrimSpace(string(out)), ":::")
	if len(parts) == 0 {
		return "", ""
	}
	appName := strings.TrimSpace(parts[0])
	winTitle := ""
	if len(parts) > 1 {
		winTitle = strings.TrimSpace(parts[1])
	}
	return appName, winTitle
}

// getMacIdleSeconds queries system idle time using ioreg.
func getMacIdleSeconds(ctx context.Context) int64 {
	cmd := exec.CommandContext(ctx, "ioreg", "-c", "IOHIDSystem")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "HIDIdleTime") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				raw := fields[len(fields)-1]
				if nanos, err := strconv.ParseInt(raw, 10, 64); err == nil {
					return nanos / 1000000000
				}
			}
		}
	}
	return 0
}
