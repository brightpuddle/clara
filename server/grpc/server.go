package grpcserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/brightpuddle/clara/pb"
	"github.com/brightpuddle/clara/server/db"
	"github.com/brightpuddle/clara/server/rag"
	"github.com/brightpuddle/clara/server/workers"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

const LinkAnalysisQueue = "link-analysis"

// Server implements pb.IngestServiceServer.
type Server struct {
	db       *db.DB
	embedder *rag.Embedder
	temporal client.Client
}

func New(database *db.DB, embedder *rag.Embedder, temporalClient client.Client) *Server {
	return &Server{db: database, embedder: embedder, temporal: temporalClient}
}

// IngestNote receives a note from the agent, chunks it, embeds it, and
// stores the result in postgres. Then triggers link analysis via Temporal.
func (s *Server) IngestNote(ctx context.Context, req *pb.IngestRequest) (*pb.IngestResponse, error) {
	doc := db.Document{
		ID:         req.DocumentId,
		Path:       req.Path,
		Title:      req.Title,
		Content:    req.Content,
		Checksum:   db.Checksum(req.Content),
		ModifiedAt: time.Unix(req.ModifiedAt, 0),
	}

	changed, err := s.db.UpsertDocument(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("upsert document: %w", err)
	}

	if !changed {
		slog.Info("document unchanged, skipping re-embed", "id", req.DocumentId)
		return &pb.IngestResponse{DocumentId: req.DocumentId, Success: true}, nil
	}

	// Chunk and embed
	chunks := rag.Chunk(req.Content)
	if len(chunks) == 0 {
		return &pb.IngestResponse{DocumentId: req.DocumentId, Success: true}, nil
	}

	embeddings, err := s.embedder.EmbedChunks(ctx, chunks)
	if err != nil {
		return &pb.IngestResponse{DocumentId: req.DocumentId, Success: false, Error: err.Error()},
			fmt.Errorf("embed chunks: %w", err)
	}

	if err := s.db.DeleteChunks(ctx, req.DocumentId); err != nil {
		return nil, fmt.Errorf("delete old chunks: %w", err)
	}
	for i, chunk := range chunks {
		if err := s.db.InsertChunk(ctx, req.DocumentId, i, chunk, embeddings[i]); err != nil {
			return nil, fmt.Errorf("insert chunk %d: %w", i, err)
		}
	}

	slog.Info("ingested note", "id", req.DocumentId, "chunks", len(chunks))

	// Trigger async link analysis workflow
	if s.temporal != nil {
		_, err = s.temporal.ExecuteWorkflow(ctx,
			client.StartWorkflowOptions{
				ID:        "link-analysis-" + req.DocumentId,
				TaskQueue: LinkAnalysisQueue,
				RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 3},
			},
			workers.LinkAnalysisWorkflow,
			req.DocumentId,
		)
		if err != nil {
			// Non-fatal: log but don't fail the ingest
			slog.Warn("failed to trigger link analysis", "err", err)
		}
	}

	return &pb.IngestResponse{DocumentId: req.DocumentId, Success: true}, nil
}

// GetPendingActions returns approved suggestions for the agent to execute.
func (s *Server) GetPendingActions(ctx context.Context, req *pb.GetActionsRequest) (*pb.GetActionsResponse, error) {
	suggestions, err := s.db.GetApprovedSuggestions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get approved suggestions: %w", err)
	}

	actions := make([]*pb.Action, 0, len(suggestions))
	for _, sug := range suggestions {
		// link_target is the title of the target doc formatted as [[title]]
		linkTarget := fmt.Sprintf("[[%s]]", sug.TargetTitle)
		actions = append(actions, &pb.Action{
			ActionId:     fmt.Sprintf("%d", sug.ID),
			Type:         pb.ActionType_ACTION_ADD_BACKLINK,
			DocumentPath: sug.SourcePath,
			LinkTarget:   linkTarget,
			Context:      sug.Context,
		})
	}

	return &pb.GetActionsResponse{Actions: actions}, nil
}

// AckAction marks a suggestion as applied or failed.
func (s *Server) AckAction(ctx context.Context, req *pb.AckRequest) (*pb.AckResponse, error) {
	var id int64
	if _, err := fmt.Sscanf(req.ActionId, "%d", &id); err != nil {
		return &pb.AckResponse{Success: false}, fmt.Errorf("invalid action_id: %w", err)
	}

	if req.Success {
		if err := s.db.MarkSuggestionApplied(ctx, id); err != nil {
			return &pb.AckResponse{Success: false}, err
		}
	} else {
		slog.Warn("action failed on agent", "id", req.ActionId, "err", req.Error)
	}

	return &pb.AckResponse{Success: true}, nil
}
