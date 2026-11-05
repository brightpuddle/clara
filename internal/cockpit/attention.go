package cockpit

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/brightpuddle/clara/internal/agents"
)

// AttentionConfig defines notification rules for agent attention events.
type AttentionConfig struct {
	Enabled          bool     `yaml:"enabled"`
	NotifyStates     []string `yaml:"notify_states"`      // blocked, needs_you, done, failed
	QuietHoursStart  int      `yaml:"quiet_hours_start"`  // hour of day 0-23 (-1 if disabled)
	QuietHoursEnd    int      `yaml:"quiet_hours_end"`    // hour of day 0-23 (-1 if disabled)
	SuppressAttached bool     `yaml:"suppress_attached"`  // suppress if attached in terminal
}

// DefaultAttentionConfig provides sensible defaults.
func DefaultAttentionConfig() AttentionConfig {
	return AttentionConfig{
		Enabled:          true,
		NotifyStates:     []string{"blocked", "needs_you", "done", "failed"},
		QuietHoursStart:  -1,
		QuietHoursEnd:    -1,
		SuppressAttached: true,
	}
}

// AttentionNotifier evaluates agent events and determines if desktop notification should fire.
type AttentionNotifier struct {
	mu              sync.RWMutex
	cfg             AttentionConfig
	attachedSession string
}

// NewAttentionNotifier creates a new AttentionNotifier.
func NewAttentionNotifier(cfg AttentionConfig) *AttentionNotifier {
	defaults := DefaultAttentionConfig()
	if len(cfg.NotifyStates) == 0 {
		cfg.NotifyStates = defaults.NotifyStates
	}
	if !cfg.Enabled && defaults.Enabled {
		// if caller specifically set Enabled, keep it
	}
	return &AttentionNotifier{
		cfg: cfg,
	}
}

// SetAttachedSession sets or clears the currently attached session to suppress notifications.
func (n *AttentionNotifier) SetAttachedSession(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.attachedSession = id
}

// inQuietHours checks if the current time falls within configured quiet hours.
func (n *AttentionNotifier) inQuietHours() bool {
	if n.cfg.QuietHoursStart < 0 || n.cfg.QuietHoursEnd < 0 {
		return false
	}
	h := time.Now().Hour()
	if n.cfg.QuietHoursStart <= n.cfg.QuietHoursEnd {
		return h >= n.cfg.QuietHoursStart && h < n.cfg.QuietHoursEnd
	}
	// Wraps midnight (e.g. 22 to 7)
	return h >= n.cfg.QuietHoursStart || h < n.cfg.QuietHoursEnd
}

// EvaluateAttention checks if an attention.requested event should produce a desktop notification.
func (n *AttentionNotifier) EvaluateAttention(ar agents.AttentionRequested, sessionID, host, project string) (bool, string, string) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.cfg.Enabled || n.inQuietHours() {
		return false, "", ""
	}
	if n.cfg.SuppressAttached && n.attachedSession == sessionID {
		return false, "", ""
	}

	title := fmt.Sprintf("Clara: Agent Needs You (%s)", host)
	if project != "" {
		title = fmt.Sprintf("Clara: %s (%s)", project, host)
	}

	body := ar.Title
	if body == "" {
		body = fmt.Sprintf("Session %s requested attention (%s)", sessionID, ar.Kind)
	}

	return true, title, body
}

// EvaluateStatus checks if a session.status transition should produce a desktop notification.
func (n *AttentionNotifier) EvaluateStatus(st agents.SessionStatus, sessionID, host, project string) (bool, string, string) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.cfg.Enabled || n.inQuietHours() {
		return false, "", ""
	}
	if n.cfg.SuppressAttached && n.attachedSession == sessionID {
		return false, "", ""
	}

	stateStr := string(st.State)
	shouldNotify := false
	for _, ns := range n.cfg.NotifyStates {
		if strings.EqualFold(ns, stateStr) {
			shouldNotify = true
			break
		}
	}

	if !shouldNotify {
		return false, "", ""
	}

	title := fmt.Sprintf("Clara: Session %s (%s)", stateStr, host)
	if project != "" {
		title = fmt.Sprintf("Clara: %s [%s]", project, stateStr)
	}

	body := fmt.Sprintf("Session %s transitioned to %s", sessionID, stateStr)
	if st.Reason != "" {
		body = fmt.Sprintf("Session %s %s: %s", sessionID, stateStr, st.Reason)
	}

	return true, title, body
}
