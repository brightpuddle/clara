package supervisor

import (
	"time"
)

// CloudEvent represents a standardized schema for all event ingress in Clara V2.
// It is designed to be compatible with the CloudEvents specification.
type CloudEvent struct {
	ID          string                 `json:"id"`
	Source      string                 `json:"source"`       // e.g., "integrations/webhook", "fsbuiltin"
	Type        string                 `json:"type"`         // e.g., "clara.sensor.file_changed"
	Time        time.Time              `json:"time"`
	Data        map[string]any         `json:"data"`
	ContentType string                 `json:"content_type"`
}

// ConvertToCloudEvent is a helper to wrap standard notify events into a CloudEvent structure.
func ConvertToCloudEvent(ev Event) CloudEvent {
	dataMap := make(map[string]any)
	if ev.Params != nil {
		if m, ok := ev.Params.(map[string]any); ok {
			dataMap = m
		} else {
			dataMap["value"] = ev.Params
		}
	}
	
	id := ""
	if idVal, ok := dataMap["id"].(string); ok {
		id = idVal
	}
	
	return CloudEvent{
		ID:          id,
		Source:      ev.Server,
		Type:        ev.Method,
		Time:        time.Now(),
		Data:        dataMap,
		ContentType: "application/json",
	}
}
