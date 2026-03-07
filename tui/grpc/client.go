// Package grpc provides the TUI's gRPC client to the Clara Agent.
package grpc

import (
	"context"
	"io"
	"time"

	"github.com/cockroachdb/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	agentv1 "github.com/brightpuddle/clara/gen/agent/v1"
	artifactv1 "github.com/brightpuddle/clara/gen/artifact/v1"
)

const dialTimeout = 5 * time.Second

// Client is a gRPC client to the Clara Agent.
type Client struct {
	conn   *grpc.ClientConn
	agent  agentv1.AgentServiceClient
	socket string
}

// New creates a new client and connects to the agent Unix socket.
func New(socketPath string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, "unix://"+socketPath, //nolint:staticcheck
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, errors.Wrapf(err, "dial agent socket %s", socketPath)
	}
	return &Client{
		conn:   conn,
		agent:  agentv1.NewAgentServiceClient(conn),
		socket: socketPath,
	}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// ListArtifacts returns all non-done artifacts sorted by heat score.
func (c *Client) ListArtifacts(ctx context.Context) ([]*artifactv1.Artifact, error) {
	resp, err := c.agent.ListArtifacts(ctx, &agentv1.ListArtifactsRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "list artifacts")
	}
	return resp.Artifacts, nil
}

// GetArtifact returns an artifact and its related neighbors.
func (c *Client) GetArtifact(ctx context.Context, id string) (*artifactv1.Artifact, []*artifactv1.Artifact, error) {
	resp, err := c.agent.GetArtifact(ctx, &agentv1.GetArtifactRequest{Id: id})
	if err != nil {
		return nil, nil, errors.Wrap(err, "get artifact")
	}
	return resp.Artifact, resp.Related, nil
}

// MarkDone marks an artifact as done.
func (c *Client) MarkDone(ctx context.Context, id string) error {
	_, err := c.agent.MarkDone(ctx, &agentv1.MarkDoneRequest{Id: id})
	return errors.Wrap(err, "mark done")
}

// Search performs a text search.
func (c *Client) Search(ctx context.Context, query string) ([]*artifactv1.Artifact, error) {
	resp, err := c.agent.Search(ctx, &agentv1.SearchRequest{Query: query, Limit: 50})
	if err != nil {
		return nil, errors.Wrap(err, "search")
	}
	return resp.Artifacts, nil
}

// Subscribe opens a streaming subscription. The returned channel receives
// ArtifactEvents until the context is cancelled or the stream closes.
func (c *Client) Subscribe(ctx context.Context) (<-chan *agentv1.ArtifactEvent, error) {
	stream, err := c.agent.Subscribe(ctx, &agentv1.SubscribeRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "subscribe")
	}

	ch := make(chan *agentv1.ArtifactEvent, 32)
	go func() {
		defer close(ch)
		for {
			ev, err := stream.Recv()
			if err == io.EOF || errors.Is(err, context.Canceled) {
				return
			}
			if err != nil {
				return
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
