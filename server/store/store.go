// Package store provides the server-side artifact and embedding store.
package store

import (
	"context"

	"github.com/cockroachdb/errors"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	"github.com/brightpuddle/clara/internal/db"
)

// Store wraps the DB with server-specific query patterns.
type Store struct {
	db *db.DB
}

// New creates a new Store backed by the given DB.
func New(database *db.DB) *Store {
	return &Store{db: database}
}

// StoreArtifact upserts an artifact and its embedding.
func (s *Store) StoreArtifact(ctx context.Context, a *artifactv1.Artifact, embedding []float32) error {
	if err := s.db.UpsertArtifact(ctx, a); err != nil {
		return errors.Wrap(err, "upsert artifact")
	}
	if len(embedding) > 0 {
		if err := s.db.UpsertEmbedding(ctx, a.Id, embedding); err != nil {
			return errors.Wrap(err, "upsert embedding")
		}
	}
	return nil
}

// SearchSimilar performs kNN vector search and returns the k most similar artifacts.
func (s *Store) SearchSimilar(ctx context.Context, queryVec []float32, k int, kinds []artifactv1.ArtifactKind) ([]*SimilarResult, error) {
	if k <= 0 {
		k = 10
	}
	knnResults, err := s.db.KNNSearch(ctx, queryVec, k, "")
	if err != nil {
		return nil, errors.Wrap(err, "knn search")
	}

	results := make([]*SimilarResult, 0, len(knnResults))
	for _, r := range knnResults {
		a, err := s.db.GetArtifact(ctx, r.ArtifactID)
		if err != nil {
			continue // artifact may have been deleted
		}
		if len(kinds) > 0 && !kindIn(a.Kind, kinds) {
			continue
		}
		results = append(results, &SimilarResult{Artifact: a, Distance: r.Distance})
	}
	return results, nil
}

// GetRelated returns the top-k artifacts most similar to the given artifact.
func (s *Store) GetRelated(ctx context.Context, artifactID string, k int) ([]*SimilarResult, error) {
	if k <= 0 {
		k = 10
	}
	// Retrieve the artifact's own embedding to use as query vector.
	vec, err := s.db.GetEmbedding(ctx, artifactID)
	if err != nil {
		return nil, errors.Wrap(err, "get embedding for artifact")
	}
	knnResults, err := s.db.KNNSearch(ctx, vec, k, artifactID)
	if err != nil {
		return nil, errors.Wrap(err, "knn search for related")
	}

	results := make([]*SimilarResult, 0, len(knnResults))
	for _, r := range knnResults {
		a, err := s.db.GetArtifact(ctx, r.ArtifactID)
		if err != nil {
			continue
		}
		results = append(results, &SimilarResult{Artifact: a, Distance: r.Distance})
	}
	return results, nil
}

// SimilarResult pairs an artifact with its similarity distance.
type SimilarResult struct {
	Artifact *artifactv1.Artifact
	Distance float64
}

func kindIn(kind artifactv1.ArtifactKind, kinds []artifactv1.ArtifactKind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}
