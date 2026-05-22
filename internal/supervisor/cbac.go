package supervisor

import (
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
)

// Capability defines a scoped privilege requested by a dynamic Actuator in Clara V2.
type Capability struct {
	Scope    string `json:"scope"`    // e.g., "fs", "net", "env"
	Action   string `json:"action"`   // e.g., "read", "write", "dial"
	Resource string `json:"resource"` // e.g., "/var/log/*", "api.webex.com"
}

// ActuatorManifest defines the declared privileges of a compiled plugin.
type ActuatorManifest struct {
	ID                  string       `json:"id"`
	Description         string       `json:"description"`
	RequiredCapabilities []Capability `json:"required_capabilities"`
}

// CBACEngine validates if the Supervisor grants a requested capability to an Actuator.
type CBACEngine struct {
	// Map of Approved Capabilities: actuatorID -> list of approved capabilities
	approved map[string][]Capability
}

// NewCBACEngine creates a new CBACEngine.
func NewCBACEngine() *CBACEngine {
	return &CBACEngine{
		approved: make(map[string][]Capability),
	}
}

// GrantCapability registers an approved privilege for a specific Actuator.
func (e *CBACEngine) GrantCapability(actuatorID string, cap Capability) {
	e.approved[actuatorID] = append(e.approved[actuatorID], cap)
}

// Authorize checks if an Actuator's declared capability matches the approved boundary.
func (e *CBACEngine) Authorize(actuatorID string, req Capability) (bool, error) {
	approvedList, ok := e.approved[actuatorID]
	if !ok {
		return false, errors.Newf("actuator %q has no approved capabilities configured", actuatorID)
	}

	for _, app := range approvedList {
		if strings.ToLower(app.Scope) != strings.ToLower(req.Scope) {
			continue
		}
		if strings.ToLower(app.Action) != strings.ToLower(req.Action) {
			continue
		}
		// Validate resource scopes (supporting wildcards)
		if matchResource(app.Resource, req.Resource) {
			return true, nil
		}
	}

	return false, errors.Newf("unauthorized capability access requested by actuator %q: scope=%s action=%s resource=%s", actuatorID, req.Scope, req.Action, req.Resource)
}

// InterceptAndDelegate checks if all declared capabilities in the manifest are authorized.
func (e *CBACEngine) InterceptAndDelegate(manifest ActuatorManifest) (bool, error) {
	for _, req := range manifest.RequiredCapabilities {
		authorized, err := e.Authorize(manifest.ID, req)
		if !authorized {
			return false, err
		}
	}
	return true, nil
}

// matchResource performs simple wildcard and host prefix matching.
func matchResource(approvedPattern, requested string) bool {
	// Exact match
	if approvedPattern == requested {
		return true
	}
	// Wildcard root check
	if approvedPattern == "*" {
		return true
	}
	// Wildcard path suffix match (e.g. "/tmp/*" matches "/tmp/logs.txt")
	if strings.HasSuffix(approvedPattern, "*") {
		prefix := strings.TrimSuffix(approvedPattern, "*")
		if strings.HasPrefix(requested, prefix) {
			return true
		}
	}
	// Wildcard glob match using filepath rules (for local filesystem paths)
	matched, err := filepath.Match(approvedPattern, requested)
	if err == nil && matched {
		return true
	}

	return false
}
