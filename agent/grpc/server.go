package grpc

import (
"context"
"net"
"strings"
"sync"
"time"

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
"github.com/brightpuddle/clara/internal/artifact"
"github.com/brightpuddle/clara/internal/db"
)

type taskwarriorWorker interface {
MarkDone(ctx context.Context, uuid string) error
}

type AgentServer struct {
agentv1.UnimplementedAgentServiceServer
db               *db.DB
serverClient     serverv1.ServerServiceClient
nativeClient     nativev1.NativeWorkerServiceClient
taskwarriorWorker taskwarriorWorker
startedAt        time.Time
mu              sync.RWMutex
subscribers     map[chan *agentv1.ArtifactEvent]struct{}
logger          zerolog.Logger
}

func NewAgentServer(database *db.DB, serverClient serverv1.ServerServiceClient, logger zerolog.Logger) *AgentServer {
return &AgentServer{
db:           database,
serverClient: serverClient,
subscribers:  make(map[chan *agentv1.ArtifactEvent]struct{}),
logger:       logger,
startedAt:    time.Now(),
}
}

func (s *AgentServer) SetNativeClient(nc nativev1.NativeWorkerServiceClient) {
s.nativeClient = nc
}

func (s *AgentServer) SetTaskwarriorWorker(w taskwarriorWorker) {
s.taskwarriorWorker = w
}

func (s *AgentServer) ListArtifacts(ctx context.Context, req *agentv1.ListArtifactsRequest) (*agentv1.ListArtifactsResponse, error) {
limit := int(req.Limit)
offset := int(req.Offset)
artifacts, err := s.db.ListArtifacts(ctx, limit, offset, req.Kinds)
if err != nil {
return nil, status.Errorf(codes.Internal, "list artifacts: %v", errors.UnwrapAll(err))
}
return &agentv1.ListArtifactsResponse{Artifacts: artifacts}, nil
}

func (s *AgentServer) GetArtifact(ctx context.Context, req *agentv1.GetArtifactRequest) (*agentv1.GetArtifactResponse, error) {
a, err := s.db.GetArtifact(ctx, req.Id)
if err != nil {
return nil, status.Errorf(codes.NotFound, "artifact %s: %v", req.Id, errors.UnwrapAll(err))
}
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
if a.Kind == artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER && s.nativeClient != nil {
_, nErr := s.nativeClient.MarkReminderDone(ctx, &nativev1.MarkReminderDoneRequest{Id: a.SourcePath})
if nErr != nil {
s.logger.Warn().Err(nErr).Str("id", a.SourcePath).Msg("mark reminder done in native worker")
}
}
if a.Kind == artifactv1.ArtifactKind_ARTIFACT_KIND_TASK && s.taskwarriorWorker != nil {
if twErr := s.taskwarriorWorker.MarkDone(ctx, a.SourcePath); twErr != nil {
s.logger.Warn().Err(twErr).Str("uuid", a.SourcePath).Msg("mark task done in taskwarrior")
}
}
s.broadcast(&agentv1.ArtifactEvent{
Type:     agentv1.EventType_EVENT_TYPE_UPDATED,
Artifact: a,
})
return &agentv1.MarkDoneResponse{Ok: true}, nil
}

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

func (s *AgentServer) Broadcast(ev *agentv1.ArtifactEvent) { s.broadcast(ev) }

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

func (s *AgentServer) GetStatus(ctx context.Context, _ *agentv1.GetStatusRequest) (*agentv1.GetStatusResponse, error) {
uptime := int64(time.Since(s.startedAt).Seconds())

agentStatus := &agentv1.ComponentStatus{
Connected:     true,
State:         "running",
UptimeSeconds: uptime,
}

serverStatus := &agentv1.ComponentStatus{State: "disconnected"}
if s.serverClient != nil {
serverStatus.Connected = true
serverStatus.State = "running"
}

nativeStatus := &agentv1.ComponentStatus{State: "disconnected"}
if s.nativeClient != nil {
nativeStatus.Connected = true
nativeStatus.State = "running"
}

counts := make(map[string]int32)
for _, kind := range []artifactv1.ArtifactKind{
artifactv1.ArtifactKind_ARTIFACT_KIND_FILE,
artifactv1.ArtifactKind_ARTIFACT_KIND_REMINDER,
artifactv1.ArtifactKind_ARTIFACT_KIND_TASK,
artifactv1.ArtifactKind_ARTIFACT_KIND_NOTE,
} {
arts, err := s.db.ListArtifacts(ctx, 0, 0, []artifactv1.ArtifactKind{kind})
if err == nil {
name := strings.ToLower(kind.String())
name = strings.TrimPrefix(name, "artifact_kind_")
counts[name] = int32(len(arts))
}
}

return &agentv1.GetStatusResponse{
Agent:          agentStatus,
Server:         serverStatus,
Native:         nativeStatus,
ArtifactCounts: counts,
}, nil
}

func (s *AgentServer) UpdateReminder(ctx context.Context, req *agentv1.UpdateReminderRequest) (*agentv1.UpdateReminderResponse, error) {
if s.nativeClient == nil {
return &agentv1.UpdateReminderResponse{Ok: false, Error: "native worker not connected"}, nil
}
nReq := &nativev1.UpdateReminderRequest{
Id:    req.Id,
Title: req.Title,
Notes: req.Notes,
}
if req.DueDate != nil {
nReq.DueDate = req.DueDate
}
resp, err := s.nativeClient.UpdateReminder(ctx, nReq)
if err != nil {
return &agentv1.UpdateReminderResponse{Ok: false, Error: err.Error()}, nil
}
return &agentv1.UpdateReminderResponse{Ok: resp.Ok, Error: resp.Error}, nil
}

func (s *AgentServer) broadcast(ev *agentv1.ArtifactEvent) {
s.mu.RLock()
defer s.mu.RUnlock()
for ch := range s.subscribers {
select {
case ch <- ev:
default:
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

func DialServer(addr string) (serverv1.ServerServiceClient, *grpc.ClientConn, error) {
conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
if err != nil {
return nil, nil, errors.Wrap(err, "dial server")
}
return serverv1.NewServerServiceClient(conn), conn, nil
}

func DialNative(socketPath string) (nativev1.NativeWorkerServiceClient, *grpc.ClientConn, error) {
conn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
if err != nil {
return nil, nil, errors.Wrap(err, "dial native worker")
}
return nativev1.NewNativeWorkerServiceClient(conn), conn, nil
}

func ListenUnix(socketPath string) (net.Listener, *grpc.Server, error) {
lis, err := net.Listen("unix", socketPath)
if err != nil {
return nil, nil, errors.Wrapf(err, "listen unix %s", socketPath)
}
srv := grpc.NewServer()
return lis, srv, nil
}

var _ = artifact.KindIcon
