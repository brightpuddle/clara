// Package db - vectors.go provides embedding storage and cosine similarity search.
package db

import (
	"context"
	"encoding/binary"
	"math"
	"sort"

	"github.com/cockroachdb/errors"
)

// UpsertEmbedding stores or updates the embedding vector for an artifact.
// The vector is stored as little-endian float32 bytes.
func (db *DB) UpsertEmbedding(ctx context.Context, artifactID string, vec []float32) error {
	blob := float32SliceToBlob(vec)
	_, err := db.ExecContext(ctx, `
		INSERT INTO artifact_embeddings (artifact_id, embedding, dim)
		VALUES (?, ?, ?)
		ON CONFLICT(artifact_id) DO UPDATE SET embedding=excluded.embedding, dim=excluded.dim
	`, artifactID, blob, len(vec))
	return errors.Wrap(err, "upsert embedding")
}

// KNNSearch returns the k nearest artifact IDs to the query vector,
// sorted by ascending cosine distance (most similar first).
// It excludes the query artifact itself if excludeID is non-empty.
func (db *DB) KNNSearch(ctx context.Context, query []float32, k int, excludeID string) ([]KNNResult, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT artifact_id, embedding FROM artifact_embeddings
	`)
	if err != nil {
		return nil, errors.Wrap(err, "query embeddings")
	}
	defer rows.Close()

	type candidate struct {
		id   string
		dist float64
	}
	var candidates []candidate

	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, errors.Wrap(err, "scan embedding row")
		}
		if id == excludeID {
			continue
		}
		vec := blobToFloat32Slice(blob)
		dist := cosineDistance(query, vec)
		candidates = append(candidates, candidate{id: id, dist: dist})
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows error")
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	if k > 0 && len(candidates) > k {
		candidates = candidates[:k]
	}

	results := make([]KNNResult, len(candidates))
	for i, c := range candidates {
		results[i] = KNNResult{ArtifactID: c.id, Distance: c.dist}
	}
	return results, nil
}

// KNNResult is a single result from a kNN search.
type KNNResult struct {
	ArtifactID string
	Distance   float64
}

// cosineDistance returns 1 - cosine_similarity(a, b).
// Returns 1.0 (maximum distance) if either vector is zero.
func cosineDistance(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1.0
	}
	var dot, normA, normB float64
	for i := range a {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	if normA == 0 || normB == 0 {
		return 1.0
	}
	return 1.0 - dot/(math.Sqrt(normA)*math.Sqrt(normB))
}

func float32SliceToBlob(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func blobToFloat32Slice(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}

// GetEmbedding retrieves the embedding vector for a specific artifact.
func (db *DB) GetEmbedding(ctx context.Context, artifactID string) ([]float32, error) {
	var blob []byte
	err := db.QueryRowContext(ctx, `
		SELECT embedding FROM artifact_embeddings WHERE artifact_id = ?
	`, artifactID).Scan(&blob)
	if err != nil {
		return nil, errors.Wrap(err, "get embedding")
	}
	return blobToFloat32Slice(blob), nil
}
