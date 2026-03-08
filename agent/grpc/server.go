// Package grpc implements the Agent gRPC server for TUI communication
// and client connections to the server and native worker.
package grpc

import (
	"context"
	"net"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	agentv1 "github.com/brightpuddle/clara/gen/agent/v1"
	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
	nativev1 "github.com/brightpuddle/clara/gen/native/v1"
	serverv1 "github.com/brightpuddle/clara/gen/server/v1"
	"github.com/brightpuddle/clara/internal/db"
	"github.com/brightpuddle/clara/internal/artifact"
)

// AgentServer implements agentv1.AgentServiceServer and serves the TUI.
type AgentServer struct {
	agentv1.UnimplementedAgentServiceServer

	db           *db.DB
	serverClient serverv1.ServerServiceClient
	nativeClient nativev1.NativeWorkerServiceClient

	// broadcaster for streaming subscribers
	mu          sync.RWMutex
	subscribers map[chan *agentv1.ArtifactEvent]struct{}

	logger zerolog.Logger
}

// NewAgentServer creates an AgentServer backed by the given DB and upstream clients.
func NewAgentServer(database *db.DB, serverClient serverv1.ServerServiceClient, logger zerolog.Logger) *AgentServer {
	return &AgentServer{
		db:           database,
		serverClient: serverClient,
		subscribers:  make(map[chan *agentv1.ArtifactEvent]struct{}),
		logger:       logger,
	}
}

// SetNativeClient attaches the Swift native worker gRPC client.
func (s *AgentServer) SetNativeClient(nc nativev1.NativeWorkerServiceClient) {
	s.nativeClient = nc
}

// ListArtifacts returns all non-done artifacts sorted by heat score.
func (s *AgentServer) ListArtifacts(ctx context.Context, req *agentv1.ListArtifactsRequest) (*agentv1.ListArtifactsResponse, error) {
	limit := int(req.Limit)
	offset := int(req.Offset)
	artifacts, err := s.db.ListArtifacts(ctx, limit, offset, req.Kinds)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list artifacts: %v", errors.UnwrapAll(err))
	}
	return &agentv1.ListArtifactsResponse{Artifacts: artifacts}, nil
}

// GetArtifact returns a single artifact and its related neighbors.
func (s *AgentServer) GetArtifact(ctx context.Context, req *agentv1.GetArtifactRequest) (*agentv1.GetArtifactResponse, error) {
	a, err := s.db.GetArtifact(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "artifact %s: %v", req.Id, errors.UnwrapAll(err))
	}

	// Fetch related via server.
	var related []*artifactv1.Artifact
	if s.serverClient != nil {
		relResp, err := s.serverClient.GetRelated(ctx, &serverv1.GetRelatedRequest{
			ArtifactId: req.Id,
			K:          10,
		})
		if err == nil {
			for _, r := range relResp.Results {
				related = append(related, r.Artifact)
			}
		}
	}

	return &agentv1.GetArtifactResponse{Artifact: a, Related: related}, nil
}

// MarkDone marks an artifact as done and writes back to native source if needed.
func (s *AgentServer) MarkDone(ctx context.Context, req *agentv1.MarkDoneRequest) (*agentv1.MarkDoneResponse, error) {
	a, err := s.db.GetArtifact(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "artifact %s not found", req.Id)
	}

	if err := s.db.MarkDone(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "mark done: %v", errors.UnwrapAll(err))
	}
	if err := s.db.LogOp(ctx, "mark_done", req.Id, map[string]string{"title": a.Title}); err != nil {
		s.logger.Warn().Err(err).Msg("log op mark_done")
	}

	// Write back to native for reminders.
	if a.Kind == artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER && s.nativeClient != nil {
		_, nErr := s.nativeClient.MarkReminderDone(ctx, &nativev1.MarkReminderDoneRequest{Id: a.SourcePath})
		if nErr != nil {
			s.logger.Warn().Err(nErr).Str("id", a.SourcePath).Msg("mark reminder done in native worker")
		}
	}

	// Broadcast to subscribers.
	s.broadcast(&agentv1.ArtifactEvent{
		Type:     agentv1.EventType_EVENT_TYPE_UPDATED,
		Artifact: a,
	})

	return &agentv1.MarkDoneResponse{Ok: true}, nil
}

// Search performs a text search across artifacts.
func (s *AgentServer) Search(ctx context.Context, req *agentv1.SearchRequest) (*agentv1.SearchResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}
	artifacts, err := s.db.SearchArtifacts(ctx, req.Query, limit, req.Kinds)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "search: %v", errors.UnwrapAll(err))
	}
	return &agentv1.SearchResponse{Artifacts: artifacts}, nil
}

// Subscribe opens a server-streaming RPC for live artifact updates.
func (s *AgentServer) Subscribe(_ *agentv1.SubscribeRequest, stream agentv1.AgentService_SubscribeServer) error {
	ch := make(chan *agentv1.ArtifactEvent, 64)
	s.addSubscriber(ch)
	defer s.removeSubscriber(ch)

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// Broadcast sends an event to all active Subscribe streams.
func (s *AgentServer) Broadcast(ev *agentv1.ArtifactEvent) {
	s.broadcast(ev)
}

// GetSystemTheme proxies to the native worker if available; falls back to dark=true.
func (s *AgentServer) GetSystemTheme(ctx context.Context, _ *agentv1.GetSystemThemeRequest) (*agentv1.GetSystemThemeResponse, error) {
	if s.nativeClient != nil {
		resp, err := s.nativeClient.GetSystemTheme(ctx, &nativev1.GetSystemThemeRequest{})
		if err == nil {
			return &agentv1.GetSystemThemeResponse{Dark: resp.Dark}, nil
		}
		s.logger.Debug().Err(err).Msg("native GetSystemTheme unavailable, defaulting to dark")
	}
	return &agentv1.GetSystemThemeResponse{Dark: true}, nil
}

func (s *AgentServer) broadcast(ev *agentv1.ArtifactEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.subscribers {
		select {
		case ch <- ev:
		default: // drop if subscriber is slow
		}
	}
}

func (s *AgentServer) addSubscriber(ch chan *agentv1.ArtifactEvent) {
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
}

func (s *AgentServer) removeSubscriber(ch chan *agentv1.ArtifactEvent) {
	s.mu.Lock()
	delete(s.subscribers, ch)
	close(ch)
	s.mu.Unlock()
}

// DialServer connects to the Clara server over TCP.
func DialServer(addr string) (serverv1.ServerServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, errors.Wrap(err, "dial server")
	}
	return serverv1.NewServerServiceClient(conn), conn, nil
}

// DialNative connects to the Swift native worker over a Unix socket.
func DialNative(socketPath string) (nativev1.NativeWorkerServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient("unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, errors.Wrap(err, "dial native worker")
	}
	return nativev1.NewNativeWorkerServiceClient(conn), conn, nil
}

// ListenUnix starts a gRPC server on a Unix domain socket.
func ListenUnix(socketPath string) (net.Listener, *grpc.Server, error) {
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "listen unix %s", socketPath)
	}
	srv := grpc.NewServer()
	return lis, srv, nil
}

// ensure unused import satisfied
var _ = artifact.KindIcon
