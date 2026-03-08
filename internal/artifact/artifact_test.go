package artifact

import (
	"math"
	"testing"
	"time"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNew(t *testing.T) {
	a := New(artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE, "Test Note", "content here", "/tmp/test.md", "filesystem")
	if a.Id == "" {
		t.Error("expected non-empty ID")
	}
	if a.Title != "Test Note" {
		t.Errorf("got title %q, want %q", a.Title, "Test Note")
	}
	if a.Kind != artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE {
		t.Errorf("got kind %v, want NOTE", a.Kind)
	}
	if a.Content != "content here" {
		t.Errorf("got content %q, want %q", a.Content, "content here")
	}
	if a.Tags == nil {
		t.Error("expected non-nil Tags slice")
	}
	if a.Metadata == nil {
		t.Error("expected non-nil Metadata map")
	}
}

func TestNew_UniqueIDs(t *testing.T) {
	a1 := New(artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE, "a", "", "", "")
	a2 := New(artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE, "b", "", "", "")
	if a1.Id == a2.Id {
		t.Error("expected unique IDs for distinct New() calls")
	}
}

func TestKindIcon(t *testing.T) {
	// These kinds have known non-empty icons.
	nonEmpty := []artifactv1.ArtifactKind{
		artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER,
		artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE,
		artifactv1.ArtifactKind_ARTIFACT_KIND_EMAIL,
		artifactv1.ArtifactKind_ARTIFACT_KIND_SUGGESTION,
		artifactv1.ArtifactKind_ARTIFACT_KIND_TASK,
	}
	for _, kind := range nonEmpty {
		icon := KindIcon(kind)
		if icon == "" {
			t.Errorf("KindIcon(%v) returned empty string", kind)
		}
	}

	// All kinds should at least not panic.
	allKinds := []artifactv1.ArtifactKind{
		artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER,
		artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE,
		artifactv1.ArtifactKind_ARTIFACT_KIND_FILE,
		artifactv1.ArtifactKind_ARTIFACT_KIND_EMAIL,
		artifactv1.ArtifactKind_ARTIFACT_KIND_BOOKMARK,
		artifactv1.ArtifactKind_ARTIFACT_KIND_LOG,
		artifactv1.ArtifactKind_ARTIFACT_KIND_SUGGESTION,
		artifactv1.ArtifactKind_ARTIFACT_KIND_TASK,
	}
	for _, kind := range allKinds {
		_ = KindIcon(kind) // must not panic
	}
}

func TestBaseUrgency(t *testing.T) {
	tests := []struct {
		kind    artifactv1.ArtifactKind
		wantMin float64
		wantMax float64
	}{
		{artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER, 0.7, 0.8},
		{artifactv1.ArtifactKind_ARTIFACT_KIND_TASK, 0.55, 0.65},
		{artifactv1.ArtifactKind_ARTIFACT_KIND_EMAIL, 0.5, 0.6},
		{artifactv1.ArtifactKind_ARTIFACT_KIND_FILE, 0.35, 0.45},
		{artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE, 0.3, 0.4},
	}
	for _, tc := range tests {
		got := baseUrgency(tc.kind)
		if got < tc.wantMin || got > tc.wantMax {
			t.Errorf("baseUrgency(%v) = %v, want [%v, %v]", tc.kind, got, tc.wantMin, tc.wantMax)
		}
	}
}

func TestRecencyFactor(t *testing.T) {
	// Very recent: close to 1.0
	recent := recencyFactor(time.Now())
	if recent < 0.99 {
		t.Errorf("recencyFactor(now) = %v, want ~1.0", recent)
	}

	// 24 hours ago: should be ~0.37 (e^-1)
	old := recencyFactor(time.Now().Add(-24 * time.Hour))
	expected := math.Exp(-1)
	if math.Abs(old-expected) > 0.01 {
		t.Errorf("recencyFactor(24h ago) = %v, want ~%v", old, expected)
	}

	// Future timestamp: treated as 0 age, should be 1.0
	future := recencyFactor(time.Now().Add(1 * time.Hour))
	if future < 0.99 {
		t.Errorf("recencyFactor(future) = %v, want ~1.0", future)
	}
}

func TestOverdueFactor_NonReminder(t *testing.T) {
	a := New(artifactv1.ArtifactKind_ARTIFACT_KIND_FILE, "file", "", "", "")
	a.DueAt = timestamppb.New(time.Now().Add(-time.Hour))
	if got := overdueFactor(a); got != 0 {
		t.Errorf("overdueFactor(non-reminder) = %v, want 0", got)
	}
}

func TestOverdueFactor_NoDueDate(t *testing.T) {
	a := New(artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER, "r", "", "", "")
	a.DueAt = nil
	if got := overdueFactor(a); got != 0 {
		t.Errorf("overdueFactor(reminder, no due) = %v, want 0", got)
	}
}

func TestOverdueFactor_DueSoon(t *testing.T) {
	a := New(artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER, "r", "", "", "")
	a.DueAt = timestamppb.New(time.Now().Add(30 * time.Minute))
	got := overdueFactor(a)
	if got != 0.5 {
		t.Errorf("overdueFactor(due in 30m) = %v, want 0.5", got)
	}
}

func TestOverdueFactor_Overdue(t *testing.T) {
	a := New(artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER, "r", "", "", "")
	a.DueAt = timestamppb.New(time.Now().Add(-48 * time.Hour))
	got := overdueFactor(a)
	if got != 1.25 {
		t.Errorf("overdueFactor(48h overdue) = %v, want 1.25 (clamped)", got)
	}
}

func TestComputeHeatScore_Range(t *testing.T) {
	kinds := []artifactv1.ArtifactKind{
		artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER,
		artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE,
		artifactv1.ArtifactKind_ARTIFACT_KIND_FILE,
		artifactv1.ArtifactKind_ARTIFACT_KIND_TASK,
	}
	for _, kind := range kinds {
		a := New(kind, "test", "", "", "")
		a.CreatedAt = timestamppb.New(time.Now())
		score := ComputeHeatScore(a)
		if score < 0.0 || score > 1.0 {
			t.Errorf("ComputeHeatScore(%v) = %v, want [0, 1]", kind, score)
		}
	}
}

func TestComputeHeatScore_ReminderHigherThanFile(t *testing.T) {
	now := timestamppb.New(time.Now())
	reminder := New(artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER, "r", "", "", "")
	reminder.CreatedAt = now
	file := New(artifactv1.ArtifactKind_ARTIFACT_KIND_FILE, "f", "", "", "")
	file.CreatedAt = now

	rScore := ComputeHeatScore(reminder)
	fScore := ComputeHeatScore(file)
	if rScore <= fScore {
		t.Errorf("expected reminder heat (%v) > file heat (%v)", rScore, fScore)
	}
}

func TestComputeHeatScore_OverdueReminderHighest(t *testing.T) {
	now := timestamppb.New(time.Now())
	overdue := New(artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER, "overdue", "", "", "")
	overdue.CreatedAt = now
	overdue.DueAt = timestamppb.New(time.Now().Add(-2 * time.Hour))

	notOverdue := New(artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER, "not overdue", "", "", "")
	notOverdue.CreatedAt = now

	oScore := ComputeHeatScore(overdue)
	nScore := ComputeHeatScore(notOverdue)
	if oScore <= nScore {
		t.Errorf("expected overdue reminder heat (%v) > not-overdue (%v)", oScore, nScore)
	}
}
