package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// Message represents a single message in a conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GenerateRequest represents a request for a completion.
type GenerateRequest struct {
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// GenerateResponse represents the response from a generation request.
type GenerateResponse struct {
	Message Message `json:"message"`
}

// VisionRequest represents a request for vision-based generation.
type VisionRequest struct {
	Messages    []Message `json:"messages"`
	ImageURL    string    `json:"image_url,omitempty"`    // URL or data:image/...
	ImageBase64 string    `json:"image_base64,omitempty"` // Raw base64 data
	Temperature float32   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// LLMIntegration is the interface for LLM services.
type LLMIntegration interface {
	Integration
	Generate(category string, req GenerateRequest) (GenerateResponse, error)
	GenerateVision(category string, req VisionRequest) (GenerateResponse, error)
	Embed(category string, input []string) ([][]float32, error)
}

// --- RPC Wrappers ---

type GenerateArgs struct {
	Category string
	Request  GenerateRequest
}

type VisionArgs struct {
	Category string
	Request  VisionRequest
}

type EmbedArgs struct {
	Category string
	Input    []string
}

type LLMIntegrationRPC struct {
	IntegrationRPC
}

func (g *LLMIntegrationRPC) Generate(category string, req GenerateRequest) (GenerateResponse, error) {
	var resp GenerateResponse
	err := g.Client.Call("Plugin.Generate", GenerateArgs{Category: category, Request: req}, &resp)
	return resp, err
}

func (g *LLMIntegrationRPC) GenerateVision(category string, req VisionRequest) (GenerateResponse, error) {
	var resp GenerateResponse
	err := g.Client.Call("Plugin.GenerateVision", VisionArgs{Category: category, Request: req}, &resp)
	return resp, err
}

func (g *LLMIntegrationRPC) Embed(category string, input []string) ([][]float32, error) {
	var resp [][]float32
	err := g.Client.Call("Plugin.Embed", EmbedArgs{Category: category, Input: input}, &resp)
	return resp, err
}

type LLMIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl LLMIntegration
}

func (s *LLMIntegrationRPCServer) Generate(args GenerateArgs, resp *GenerateResponse) error {
	var err error
	*resp, err = s.Impl.Generate(args.Category, args.Request)
	return err
}

func (s *LLMIntegrationRPCServer) GenerateVision(args VisionArgs, resp *GenerateResponse) error {
	var err error
	*resp, err = s.Impl.GenerateVision(args.Category, args.Request)
	return err
}

func (s *LLMIntegrationRPCServer) Embed(args EmbedArgs, resp *[][]float32) error {
	var err error
	*resp, err = s.Impl.Embed(args.Category, args.Input)
	return err
}

func (s *LLMIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *LLMIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *LLMIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *LLMIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type LLMIntegrationPlugin struct {
	Impl LLMIntegration
}

func (p *LLMIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &LLMIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *LLMIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &LLMIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
