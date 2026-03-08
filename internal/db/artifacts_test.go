package db

import (
	"context"
	"testing"
	"time"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func makeArtifact(id, title string, kind artifactv1.ArtifactKind) *artifactv1.Artifact {
	return &artifactv1.Artifact{
		Id:        id,
		Kind:      kind,
		Title:     title,
		Content:   "test content",
		Tags:      []string{},
		Metadata:  map[string]string{},
		CreatedAt: timestamppb.New(time.Now()),
		UpdatedAt: timestamppb.New(time.Now()),
		HeatScore: 0.5,
	}
}

func TestUpsertAndGetArtifact(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("test-1", "My Note", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := db.GetArtifact(ctx, "test-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "My Note" {
		t.Errorf("got title %q, want %q", got.Title, "My Note")
	}
	if got.Kind != artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE {
		t.Errorf("got kind %v, want NOTE", got.Kind)
	}
}

func TestUpsertArtifact_Update(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("upd-1", "Original", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	a.Title = "Updated"
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	got, err := db.GetArtifact(ctx, "upd-1")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Title != "Updated" {
		t.Errorf("got title %q, want %q", got.Title, "Updated")
	}
}

func TestListArtifacts_Empty(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	arts, err := db.ListArtifacts(ctx, 0, 0, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(arts) != 0 {
		t.Errorf("expected 0 artifacts, got %d", len(arts))
	}
}

func TestListArtifacts_All(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	for i, title := range []string{"a", "b", "c"} {
		a := makeArtifact(title, title, artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
		a.HeatScore = float64(i) * 0.1
		if err := db.UpsertArtifact(ctx, a); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	arts, err := db.ListArtifacts(ctx, 0, 0, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(arts) != 3 {
		t.Errorf("expected 3 artifacts, got %d", len(arts))
	}
}

func TestListArtifacts_KindFilter(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := db.UpsertArtifact(ctx, makeArtifact("note-1", "note", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertArtifact(ctx, makeArtifact("rem-1", "reminder", artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER)); err != nil {
		t.Fatal(err)
	}

	arts, err := db.ListArtifacts(ctx, 0, 0, []artifactv1.ArtifactKind{artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE})
	if err != nil {
		t.Fatalf("list with filter: %v", err)
	}
	if len(arts) != 1 {
		t.Errorf("expected 1 note, got %d", len(arts))
	}
	if arts[0].Kind != artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE {
		t.Errorf("got kind %v, want NOTE", arts[0].Kind)
	}
}

func TestListArtifacts_ExcludesDone(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("done-1", "done item", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkDone(ctx, "done-1"); err != nil {
		t.Fatal(err)
	}

	arts, err := db.ListArtifacts(ctx, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 0 {
		t.Errorf("expected 0 (done excluded), got %d", len(arts))
	}
}

func TestMarkDone(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("m-1", "item", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkDone(ctx, "m-1"); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	got, err := db.GetArtifact(ctx, "m-1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Done {
		t.Error("expected artifact to be marked done")
	}
}

func TestSearchArtifacts(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("s-1", "Project Planning", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	a.Content = "quarterly review notes"
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}

	b := makeArtifact("s-2", "Shopping List", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	b.Content = "groceries"
	if err := db.UpsertArtifact(ctx, b); err != nil {
		t.Fatal(err)
	}

	results, err := db.SearchArtifacts(ctx, "planning", 10, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].Id != "s-1" {
		t.Errorf("got id %q, want s-1", results[0].Id)
	}
}

func TestSearchArtifacts_ContentMatch(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("c-1", "Notes", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	a.Content = "important quarterly review"
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}

	results, err := db.SearchArtifacts(ctx, "quarterly", 10, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestSearchArtifacts_ExcludesDone(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("sd-1", "searchable done", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkDone(ctx, "sd-1"); err != nil {
		t.Fatal(err)
	}

	results, err := db.SearchArtifacts(ctx, "searchable", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (done excluded), got %d", len(results))
	}
}

func TestListArtifacts_HeatScoreOrder(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	low := makeArtifact("heat-low", "low", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	low.HeatScore = 0.1
	high := makeArtifact("heat-high", "high", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	high.HeatScore = 0.9

	if err := db.UpsertArtifact(ctx, low); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertArtifact(ctx, high); err != nil {
		t.Fatal(err)
	}

	arts, err := db.ListArtifacts(ctx, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) < 2 {
		t.Fatal("expected at least 2 artifacts")
	}
	if arts[0].Id != "heat-high" {
		t.Errorf("expected high heat first, got %q", arts[0].Id)
	}
}

func TestArtifactWithDueDate(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	due := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	a := makeArtifact("due-1", "due reminder", artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER)
	a.DueAt = timestamppb.New(due)
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetArtifact(ctx, "due-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DueAt == nil {
		t.Fatal("expected non-nil DueAt")
	}
	if !got.DueAt.AsTime().Equal(due) {
		t.Errorf("got due_at %v, want %v", got.DueAt.AsTime(), due)
	}
}
