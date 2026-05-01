package contract

import (
	"context"
	"io"

	"github.com/brightpuddle/clara/pkg/contract/proto"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

type IntegrationGRPCPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	Impl Integration
}

func (p *IntegrationGRPCPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterIntegrationServer(s, &GRPCServer{
		Impl: p.Impl,
	})
	return nil
}

func (p *IntegrationGRPCPlugin) GRPCClient(
	ctx context.Context,
	broker *plugin.GRPCBroker,
	c *grpc.ClientConn,
) (interface{}, error) {
	return &GRPCClient{
		client: proto.NewIntegrationClient(c),
	}, nil
}

type GRPCServer struct {
	proto.UnimplementedIntegrationServer
	Impl Integration
}

func (m *GRPCServer) Configure(
	ctx context.Context,
	req *proto.ConfigureRequest,
) (*proto.ConfigureResponse, error) {
	err := m.Impl.Configure(req.Config)
	return &proto.ConfigureResponse{}, err
}

func (m *GRPCServer) Description(
	ctx context.Context,
	req *proto.DescriptionRequest,
) (*proto.DescriptionResponse, error) {
	desc, err := m.Impl.Description()
	return &proto.DescriptionResponse{Description: desc}, err
}

func (m *GRPCServer) Tools(
	ctx context.Context,
	req *proto.ToolsRequest,
) (*proto.ToolsResponse, error) {
	tools, err := m.Impl.Tools()
	return &proto.ToolsResponse{Tools: tools}, err
}

func (m *GRPCServer) CallTool(
	ctx context.Context,
	req *proto.CallToolRequest,
) (*proto.CallToolResponse, error) {
	res, err := m.Impl.CallTool(req.Name, req.Args)
	return &proto.CallToolResponse{Result: res}, err
}

func (m *GRPCServer) StreamEvents(
	req *proto.StreamEventsRequest,
	srv proto.Integration_StreamEventsServer,
) error {
	streamer, ok := m.Impl.(EventStreamer)
	if !ok {
		return nil
	}

	events, err := streamer.StreamEvents()
	if err != nil {
		return err
	}

	for {
		select {
		case <-srv.Context().Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if err := srv.Send(&proto.Event{
				Name: ev.Name,
				Data: ev.Data,
			}); err != nil {
				return err
			}
		}
	}
}

type GRPCClient struct {
	client proto.IntegrationClient
}

func (m *GRPCClient) Configure(config []byte) error {
	_, err := m.client.Configure(context.Background(), &proto.ConfigureRequest{Config: config})
	return err
}

func (m *GRPCClient) Description() (string, error) {
	res, err := m.client.Description(context.Background(), &proto.DescriptionRequest{})
	if err != nil {
		return "", err
	}
	return res.Description, nil
}

func (m *GRPCClient) Tools() ([]byte, error) {
	res, err := m.client.Tools(context.Background(), &proto.ToolsRequest{})
	if err != nil {
		return nil, err
	}
	return res.Tools, nil
}

func (m *GRPCClient) CallTool(name string, args []byte) ([]byte, error) {
	res, err := m.client.CallTool(
		context.Background(),
		&proto.CallToolRequest{Name: name, Args: args},
	)
	if err != nil {
		return nil, err
	}
	return res.Result, nil
}

func (m *GRPCClient) StreamEvents() (<-chan Event, error) {
	stream, err := m.client.StreamEvents(context.Background(), &proto.StreamEventsRequest{})
	if err != nil {
		return nil, err
	}

	events := make(chan Event)
	go func() {
		defer close(events)
		for {
			ev, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			events <- Event{
				Name: ev.Name,
				Data: ev.Data,
			}
		}
	}()

	return events, nil
}
