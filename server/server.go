// Package server implements the Clara Server gRPC service.
package server

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	serverv1 "github.com/brightpuddle/clara/gen/server/v1"
	"github.com/brightpuddle/clara/server/store"
)

// Server implements serverv1.ServerServiceServer.
type Server struct {
	serverv1.UnimplementedServerServiceServer
	store  *store.Store
	logger zerolog.Logger
}

// New creates a new Server.
func New(s *store.Store, logger zerolog.Logger) *Server {
	return &Server{store: s, logger: logger}
}

// StoreArtifact persists an artifact and its embedding vector.
func (s *Server) StoreArtifact(ctx context.Context, req *serverv1.StoreArtifactRequest) (*serverv1.StoreArtifactResponse, error) {
	if req.Artifact == nil {
		return nil, status.Error(codes.InvalidArgument, "artifact is required")
	}

	if err := s.store.StoreArtifact(ctx, req.Artifact, req.Embedding); err != nil {
		s.logger.Error().Err(err).Str("artifact_id", req.Artifact.Id).Msg("StoreArtifact failed")
		return nil, status.Errorf(codes.Internal, "store artifact: %v", err)
	}

	s.logger.Debug().Str("id", req.Artifact.Id).Str("title", req.Artifact.Title).Msg("stored artifact")
	return &serverv1.StoreArtifactResponse{Ok: true}, nil
}

// SearchSimilar performs kNN vector similarity search.
func (s *Server) SearchSimilar(ctx context.Context, req *serverv1.SearchSimilarRequest) (*serverv1.SearchSimilarResponse, error) {
	if len(req.Embedding) == 0 {
		return nil, status.Error(codes.InvalidArgument, "embedding is required")
	}

	k := int(req.K)
	if k <= 0 {
		k = 10
	}

	results, err := s.store.SearchSimilar(ctx, req.Embedding, k, req.Kinds)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "search similar: %v", errors.UnwrapAll(err))
	}

	resp := &serverv1.SearchSimilarResponse{
		Results: make([]*serverv1.SimilarResult, len(results)),
	}
	for i, r := range results {
		resp.Results[i] = &serverv1.SimilarResult{
			Artifact: r.Artifact,
			Distance: r.Distance,
		}
	}
	return resp, nil
}

// GetRelated returns top-k artifacts similar to the given artifact.
func (s *Server) GetRelated(ctx context.Context, req *serverv1.GetRelatedRequest) (*serverv1.GetRelatedResponse, error) {
	if req.ArtifactId == "" {
		return nil, status.Error(codes.InvalidArgument, "artifact_id is required")
	}

	k := int(req.K)
	if k <= 0 {
		k = 10
	}

	results, err := s.store.GetRelated(ctx, req.ArtifactId, k)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get related: %v", errors.UnwrapAll(err))
	}

	resp := &serverv1.GetRelatedResponse{
		Results: make([]*serverv1.SimilarResult, len(results)),
	}
	for i, r := range results {
		resp.Results[i] = &serverv1.SimilarResult{
			Artifact: r.Artifact,
			Distance: r.Distance,
		}
	}
	return resp, nil
}

// kindToProto converts an internal ArtifactKind int to proto enum.
func kindToProto(k int) artifactv1.ArtifactKind {
	return artifactv1.ArtifactKind(k)
}
