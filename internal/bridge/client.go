// Package bridge provides the Go gRPC client that communicates with the
// ClaraBridge Swift process over a Unix Domain Socket.
package bridge

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"github.com/brightpuddle/clara/internal/bridge/gen"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is a gRPC client for the Swift ClaraBridge process.
type Client struct {
	conn   *grpc.ClientConn
	svc    bridgepb.BridgeServiceClient
	log    zerolog.Logger
}

// New dials the ClaraBridge Unix Domain Socket and returns a ready Client.
// The socketPath should match BridgeConfig.SocketPath from the daemon config.
func New(socketPath string, log zerolog.Logger) (*Client, error) {
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", addr)
	}

	conn, err := grpc.NewClient(
		socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	if err != nil {
		return nil, errors.Wrap(err, "dial bridge socket")
	}

	return &Client{
		conn: conn,
		svc:  bridgepb.NewBridgeServiceClient(conn),
		log:  log.With().Str("component", "bridge_client").Logger(),
	}, nil
}

// Close releases the gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Ping checks that the Swift bridge process is alive.
func (c *Client) Ping(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := c.svc.Ping(ctx, &bridgepb.PingRequest{})
	if err != nil {
		return "", errors.Wrap(err, "bridge ping")
	}
	return resp.Version, nil
}

// CallTool invokes a named native tool on the Swift bridge and returns the
// JSON-decoded result.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, errors.Wrap(err, "marshal tool args")
	}

	resp, err := c.svc.CallTool(ctx, &bridgepb.CallToolRequest{
		Name:     name,
		ArgsJson: string(argsJSON),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "bridge call tool %q", name)
	}
	if resp.Error != "" {
		return nil, errors.Newf("bridge tool %q error: %s", name, resp.Error)
	}

	var result any
	if err := json.Unmarshal([]byte(resp.ResultJson), &result); err != nil {
		return nil, errors.Wrap(err, "unmarshal bridge tool result")
	}
	return result, nil
}

// AsRegistryTool returns a registry.Tool-compatible function that delegates
// all calls to the named native tool on the Swift bridge.
func (c *Client) AsRegistryTool(toolName string) func(ctx context.Context, args map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		return c.CallTool(ctx, toolName, args)
	}
}
