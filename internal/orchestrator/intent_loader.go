package orchestrator

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// LoadIntentFile parses an intent/actuator metadata schema from JSON or YAML data.
// In Clara V2, all actuators are natively compiled binaries; this parses their registration profiles.
func LoadIntentFile(path string, data []byte, namespaces []string) (*Intent, error) {
	var intent Intent
	if err := json.Unmarshal(data, &intent); err != nil {
		if err := yaml.Unmarshal(data, &intent); err != nil {
			return nil, errors.Wrap(err, "failed to decode intent metadata")
		}
	}
	
	// Default to native Go binary actuators
	intent.WorkflowType = WorkflowTypeNative
	
	// Derive the ID from the filename if not explicitly provided
	if intent.ID == "" {
		intent.ID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	return &intent, nil
}
