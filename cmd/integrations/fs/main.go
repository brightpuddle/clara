package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
	"github.com/mark3labs/mcp-go/mcp"
)

type FSPlugin struct {
	server *Server
}

func (p *FSPlugin) Configure(config []byte) error {
	// Currently no dynamic config needed, just initialize
	p.server = New(context.Background())
	return nil
}

func (p *FSPlugin) Description() (string, error) {
	return Description, nil
}

func (p *FSPlugin) Tools() ([]byte, error) {
	if p.server == nil {
		return nil, fmt.Errorf("FSPlugin not configured")
	}
	serverTools := p.server.ListTools()
	var tools []mcp.Tool
	for _, st := range serverTools {
		tools = append(tools, st.Tool)
	}
	return json.Marshal(tools)
}

func (p *FSPlugin) CallTool(name string, args []byte) ([]byte, error) {
	if p.server == nil {
		return nil, fmt.Errorf("FSPlugin not configured")
	}
	
	// Strip prefix if necessary, but plugins.go prefixes names for the registry,
	// the original tool name is passed back to CallTool.
	serverTools := p.server.ListTools()
	st, ok := serverTools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q not found in fs plugin", name)
	}

	var parsedArgs map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &parsedArgs); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = parsedArgs

	res, err := st.Handler(context.Background(), req)
	if err != nil {
		return nil, err
	}

	if res.IsError {
		var texts []string
		for _, c := range res.Content {
			if tc, ok := c.(mcp.TextContent); ok {
				texts = append(texts, tc.Text)
			}
		}
		return nil, fmt.Errorf("%s", strings.Join(texts, "\n"))
	}

	if res.StructuredContent != nil {
		return json.Marshal(res.StructuredContent)
	}

	if len(res.Content) == 1 {
		if tc, ok := res.Content[0].(mcp.TextContent); ok {
			return []byte(tc.Text), nil
		}
	}

	return json.Marshal(res.Content)
}

func (p *FSPlugin) ReadFile(path string) ([]byte, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = "read_file"
	req.Params.Arguments = map[string]any{"path": path}
	res, err := handleReadFile(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if res.IsError {
		return nil, fmt.Errorf("read_file error")
	}
	if len(res.Content) == 1 {
		if tc, ok := res.Content[0].(mcp.TextContent); ok {
			return []byte(tc.Text), nil
		}
	}
	return nil, fmt.Errorf("unexpected read_file result")
}

func (p *FSPlugin) WriteFile(path string, content []byte) error {
	req := mcp.CallToolRequest{}
	req.Params.Name = "write_file"
	req.Params.Arguments = map[string]any{
		"path":    path,
		"content": string(content),
	}
	res, err := handleWriteFile(context.Background(), req)
	if err != nil {
		return err
	}
	if res.IsError {
		return fmt.Errorf("write_file error")
	}
	return nil
}

func main() {
	impl := &FSPlugin{}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"fs": &contract.FSIntegrationPlugin{Impl: impl},
		},
	})
}
