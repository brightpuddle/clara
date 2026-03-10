package db

import (
	"context"
	"math"
	"testing"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
)

func TestUpsertAndGetEmbedding(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("emb-1", "Embedding Test", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	if err := db.UpsertEmbedding(ctx, "emb-1", vec); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	got, err := db.GetEmbedding(ctx, "emb-1")
	if err != nil {
		t.Fatalf("get embedding: %v", err)
	}
	if len(got) != len(vec) {
		t.Fatalf("got %d dims, want %d", len(got), len(vec))
	}
	for i := range vec {
		if math.Abs(float64(got[i]-vec[i])) > 1e-6 {
			t.Errorf("dim %d: got %v, want %v", i, got[i], vec[i])
		}
	}
}

func TestUpsertEmbedding_Upsert(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("emb-u", "Upsert test", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	if err := db.UpsertArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}

	v1 := []float32{1.0, 0.0}
	v2 := []float32{0.0, 1.0}

	if err := db.UpsertEmbedding(ctx, "emb-u", v1); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertEmbedding(ctx, "emb-u", v2); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetEmbedding(ctx, "emb-u")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(got[0])) > 1e-6 || math.Abs(float64(got[1])-1.0) > 1e-6 {
		t.Errorf("expected updated embedding [0, 1], got %v", got)
	}
}

func TestKNNSearch_Basic(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Insert 3 artifacts with embeddings.
	ids := []string{"knn-1", "knn-2", "knn-3"}
	vecs := [][]float32{
		{1.0, 0.0, 0.0},
		{0.0, 1.0, 0.0},
		{0.0, 0.0, 1.0},
	}
	for i, id := range ids {
		a := makeArtifact(id, "item "+id, artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
		if err := db.UpsertArtifact(ctx, a); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertEmbedding(ctx, id, vecs[i]); err != nil {
			t.Fatal(err)
		}
	}

	// Query close to knn-1
	query := []float32{0.9, 0.1, 0.0}
	results, err := db.KNNSearch(ctx, query, 2, "")
	if err != nil {
		t.Fatalf("KNNSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].ArtifactID != "knn-1" {
		t.Errorf("expected knn-1 as nearest, got %q", results[0].ArtifactID)
	}
}

func TestKNNSearch_ExcludesID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("ex-1", "excluded", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	b := makeArtifact("ex-2", "included", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	for _, art := range []*artifactv1.Artifact{a, b} {
		if err := db.UpsertArtifact(ctx, art); err != nil {
			t.Fatal(err)
		}
	}

	vec := []float32{1.0, 0.0}
	if err := db.UpsertEmbedding(ctx, "ex-1", vec); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertEmbedding(ctx, "ex-2", []float32{0.9, 0.1}); err != nil {
		t.Fatal(err)
	}

	results, err := db.KNNSearch(ctx, vec, 10, "ex-1")
	if err != nil {
		t.Fatalf("KNNSearch: %v", err)
	}
	for _, r := range results {
		if r.ArtifactID == "ex-1" {
			t.Error("expected ex-1 to be excluded from results")
		}
	}
}

func TestKNNSearch_Empty(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	results, err := db.KNNSearch(ctx, []float32{1.0, 0.0}, 5, "")
	if err != nil {
		t.Fatalf("KNNSearch on empty db: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on empty db, got %d", len(results))
	}
}

func TestCosineDistance(t *testing.T) {
	tests := []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0}, []float32{1, 0}, 0.0},  // identical → distance 0
		{[]float32{1, 0}, []float32{0, 1}, 1.0},  // orthogonal → distance 1
		{[]float32{1, 0}, []float32{-1, 0}, 2.0}, // opposite → distance 2
		{[]float32{1, 1}, []float32{1, 1}, 0.0},  // identical → distance 0
		{nil, []float32{1, 0}, 1.0},              // nil → max distance
		{[]float32{}, []float32{}, 1.0},          // empty → max distance
		{[]float32{0, 0}, []float32{1, 0}, 1.0},  // zero vector → max distance
	}
	for _, tc := range tests {
		got := cosineDistance(tc.a, tc.b)
		if math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("cosineDistance(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestFloat32BlobRoundtrip(t *testing.T) {
	original := []float32{1.5, -0.5, 3.14159, 0.0, -100.0}
	blob := float32SliceToBlob(original)
	back := blobToFloat32Slice(blob)

	if len(back) != len(original) {
		t.Fatalf("got %d elems, want %d", len(back), len(original))
	}
	for i := range original {
		if back[i] != original[i] {
			t.Errorf("elem %d: got %v, want %v", i, back[i], original[i])
		}
	}
}

func TestKNNSearch_ExcludesDoneArtifacts(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := makeArtifact("done-knn", "done", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	b := makeArtifact("active-knn", "active", artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE)
	for _, art := range []*artifactv1.Artifact{a, b} {
		if err := db.UpsertArtifact(ctx, art); err != nil {
			t.Fatal(err)
		}
	}
	vec := []float32{1.0, 0.0}
	if err := db.UpsertEmbedding(ctx, "done-knn", vec); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertEmbedding(ctx, "active-knn", vec); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkDone(ctx, "done-knn"); err != nil {
		t.Fatal(err)
	}

	results, err := db.KNNSearch(ctx, vec, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.ArtifactID == "done-knn" {
			t.Error("done artifact should not appear in KNN results")
		}
	}
	if len(results) != 1 || results[0].ArtifactID != "active-knn" {
		t.Errorf("expected only active-knn in results, got %v", results)
	}
}
