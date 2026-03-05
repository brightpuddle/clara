package ingest

import (
	"context"
	"fmt"

	"github.com/brightpuddle/clara/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps the gRPC IngestService client.
type Client struct {
	conn    *grpc.ClientConn
	client  pb.IngestServiceClient
	agentID string
}

func NewClient(serverAddr string) (*Client, error) {
	conn, err := grpc.NewClient(serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", serverAddr, err)
	}
	return &Client{
		conn:    conn,
		client:  pb.NewIngestServiceClient(conn),
		agentID: "default",
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

// IngestNote sends a note's content to the server for embedding.
func (c *Client) IngestNote(ctx context.Context, req *pb.IngestRequest) error {
	resp, err := c.client.IngestNote(ctx, req)
	if err != nil {
		return fmt.Errorf("IngestNote: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("IngestNote: server error: %s", resp.Error)
	}
	return nil
}

// GetPendingActions polls the server for approved actions to execute.
func (c *Client) GetPendingActions(ctx context.Context) ([]*pb.Action, error) {
	resp, err := c.client.GetPendingActions(ctx, &pb.GetActionsRequest{AgentId: c.agentID})
	if err != nil {
		return nil, fmt.Errorf("GetPendingActions: %w", err)
	}
	return resp.Actions, nil
}

// AckAction reports whether an action was successfully applied.
func (c *Client) AckAction(ctx context.Context, actionID string, success bool, errMsg string) error {
	_, err := c.client.AckAction(ctx, &pb.AckRequest{
		ActionId: actionID,
		Success:  success,
		Error:    errMsg,
	})
	return err
}
