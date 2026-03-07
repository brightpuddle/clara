// Package artifact provides the core data model and heat score computation.
package artifact

import (
	"math"
	"time"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	"github.com/google/uuid"
)

// New creates a new Artifact with a generated UUID and current timestamps.
func New(kind artifactv1.ArtifactKind, title, content, sourcePath, sourceApp string) *artifactv1.Artifact {
	return &artifactv1.Artifact{
		Id:         uuid.New().String(),
		Kind:       kind,
		Title:      title,
		Content:    content,
		SourcePath: sourcePath,
		SourceApp:  sourceApp,
		Tags:       []string{},
		Metadata:   map[string]string{},
	}
}

// ComputeHeatScore calculates the urgency/priority score for an artifact.
// Score range: [0.0, 1.0] — higher means more urgent / needs more attention.
//
// Formula: baseUrgency * recencyFactor * (1 + overdueBonus)
func ComputeHeatScore(a *artifactv1.Artifact) float64 {
	base := baseUrgency(a.Kind)
	recency := recencyFactor(a.CreatedAt.AsTime())
	overdue := overdueFactor(a)
	score := base * recency * (1.0 + overdue)
	// Clamp to [0, 1]
	return math.Min(1.0, math.Max(0.0, score))
}

// baseUrgency returns the kind-specific urgency weight.
func baseUrgency(kind artifactv1.ArtifactKind) float64 {
	switch kind {
	case artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER:
		return 0.75
	case artifactv1.ArtifactKind_ARTIFACT_KIND_LOG:
		return 0.65
	case artifactv1.ArtifactKind_ARTIFACT_KIND_EMAIL:
		return 0.55
	case artifactv1.ArtifactKind_ARTIFACT_KIND_FILE:
		return 0.40
	case artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE:
		return 0.35
	case artifactv1.ArtifactKind_ARTIFACT_KIND_BOOKMARK:
		return 0.25
	case artifactv1.ArtifactKind_ARTIFACT_KIND_SUGGESTION:
		return 0.20
	default:
		return 0.30
	}
}

// recencyFactor returns an exponential decay factor based on artifact age.
// Items created within the last hour are close to 1.0; 24h old ≈ 0.37.
func recencyFactor(createdAt time.Time) float64 {
	ageHours := time.Since(createdAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	return math.Exp(-ageHours / 24.0)
}

// overdueFactor returns an urgency bonus for overdue reminders.
// Returns 0 for non-reminders or reminders with no due date.
// Returns up to 1.25 for significantly overdue items.
func overdueFactor(a *artifactv1.Artifact) float64 {
	if a.Kind != artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER {
		return 0
	}
	if a.DueAt == nil {
		return 0
	}
	overdueMins := time.Since(a.DueAt.AsTime()).Minutes()
	if overdueMins <= 0 {
		// Due in the future: small positive urgency bump
		dueSoonMins := -overdueMins
		if dueSoonMins < 60 {
			return 0.5 // due within an hour
		}
		return 0
	}
	// Overdue: scale from 0 (just overdue) to 1.25 (24h+ overdue)
	return math.Min(1.25, overdueMins/1440.0*1.25)
}

// KindIcon returns the nerd font icon for an artifact kind.
func KindIcon(kind artifactv1.ArtifactKind) string {
	switch kind {
	case artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER:
		return "󰃰" // nf-md-calendar_check
	case artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE:
		return "󰎞" // nf-md-note_text
	case artifactv1.ArtifactKind_ARTIFACT_KIND_FILE:
		return "" // nf-fa-file
	case artifactv1.ArtifactKind_ARTIFACT_KIND_EMAIL:
		return "󰇮" // nf-md-email
	case artifactv1.ArtifactKind_ARTIFACT_KIND_BOOKMARK:
		return "" // nf-fa-bookmark
	case artifactv1.ArtifactKind_ARTIFACT_KIND_LOG:
		return "" // nf-fa-terminal
	case artifactv1.ArtifactKind_ARTIFACT_KIND_SUGGESTION:
		return "󰛩" // nf-md-lightbulb_on
	default:
		return "" // nf-fa-circle
	}
}
