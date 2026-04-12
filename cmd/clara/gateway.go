package main

import (
	"context"
	"fmt"
	"os"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/toolcatalog"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
)

func runGateway(ctx context.Context) error {
	if !isRunning(cfg.ControlSocketPath()) {
		return errors.New("clara daemon is not running")
	}

	mcpSrv := server.NewMCPServer(
		"clara-gateway",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithInstructions("Clara local MCP gateway (stdio proxy)"),
	)

	// Add tools.
	if err := addExposedTools(mcpSrv); err != nil {
		return err
	}

	// Add intents if configured.
	if cfg.StdioMCP != nil && len(cfg.StdioMCP.ExposeIntents) > 0 {
		addIntentTools(mcpSrv)
	}

	return serveMCP(ctx, mcpSrv)
}

func addExposedTools(mcpSrv *server.MCPServer) error {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodToolList})
	if err != nil {
		return fmt.Errorf("tool list request failed: %w", err)
	}

	// We'll need to parse the tool list to get the specs.
	// We'll use a modified version of tool.go's decodeToolList.
	tools, err := decodeToolList(resp.Data)
	if err != nil {
		return err
	}

	exposedCount := 0
	for _, tool := range tools {
		if cfg.StdioMCP != nil && !cfg.StdioMCP.MatchesTool(tool.Name) {
			continue
		}

		spec := mapToolToMCP(tool)
		mcpSrv.AddTool(spec, createToolHandler(tool.Name))
		exposedCount++
	}

	fmt.Fprintf(os.Stderr, "clara-gateway: exposed %d tools\n", exposedCount)
	return nil
}

func mapToolToMCP(tool toolcatalog.Tool) mcp.Tool {
	properties := make(map[string]any)
	required := make([]string, 0)

	for _, p := range tool.Parameters {
		properties[p.Name] = map[string]any{
			"type":        p.Type,
			"description": p.Description,
		}
		if p.Required {
			required = append(required, p.Name)
		}
	}

	return mcp.Tool{
		Name:        tool.Name,
		Description: tool.Description,
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: properties,
			Required:   required,
		},
	}
}

func createToolHandler(toolName string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := sendRawRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodToolCall,
			Params: map[string]any{
				"name": toolName,
				"args": req.GetArguments(),
			},
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("tool call request failed: %v", err)), nil
		}
		if resp.Error != "" {
			return mcp.NewToolResultError(resp.Error), nil
		}

		// Tool result can be anything, but mcp-go expects CallToolResult content.
		// We'll marshal it to JSON and present it as text if it's not already structured.
		if resp.Data == nil {
			return mcp.NewToolResultText("null"), nil
		}

		return mcp.NewToolResultStructuredOnly(resp.Data), nil
	}
}

func addIntentTools(mcpSrv *server.MCPServer) {
	mcpSrv.AddTool(mcp.NewTool(
		"clara_intent_list",
		mcp.WithDescription("List exposed Clara intents"),
	), handleIntentList)

	mcpSrv.AddTool(mcp.NewTool(
		"clara_intent_start",
		mcp.WithDescription("Start a Clara intent task"),
		mcp.WithString("id", mcp.Required(), mcp.Description("The intent ID")),
		mcp.WithString("task", mcp.Description("Optional task name")),
	), handleIntentStart)

	mcpSrv.AddTool(mcp.NewTool(
		"clara_intent_logs",
		mcp.WithDescription("Get the logs of a Clara intent"),
		mcp.WithString("id", mcp.Description("Optional intent ID to filter logs")),
	), handleIntentLogs)
}

func handleIntentList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodList})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("intent list request failed: %v", err)), nil
	}

	items, ok := resp.Data.([]any)
	if !ok {
		return mcp.NewToolResultError("invalid intent list response"), nil
	}

	var exposed []any
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		intentID, _ := m["intent_id"].(string)
		if cfg.StdioMCP.MatchesIntent(intentID) {
			exposed = append(exposed, m)
		}
	}

	return mcp.NewToolResultStructuredOnly(exposed), nil
}

func handleIntentStart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	intentID, _ := req.GetArguments()["id"].(string)
	if intentID == "" {
		return mcp.NewToolResultError("intent id is required"), nil
	}

	if !cfg.StdioMCP.MatchesIntent(intentID) {
		return mcp.NewToolResultError(fmt.Sprintf("intent %q is not exposed", intentID)), nil
	}

	params := map[string]any{"id": intentID}
	if task, ok := req.GetArguments()["task"].(string); ok && task != "" {
		params["task"] = task
	}

	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodStart,
		Params: params,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("intent start failed: %v", err)), nil
	}

	return mcp.NewToolResultText(resp.Message), nil
}

func handleIntentLogs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	intentID, _ := req.GetArguments()["id"].(string)
	if intentID != "" && !cfg.StdioMCP.MatchesIntent(intentID) {
		return mcp.NewToolResultError(fmt.Sprintf("intent %q is not exposed", intentID)), nil
	}

	// We'll open the DB directly for logs, similar to the CLI.
	db, err := store.Open(cfg.DBPath(), zerolog.Nop())
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to open store: %v", err)), nil
	}
	defer db.Close()

	// Get recent events. For simplicity, we'll return the last 20 events.
	events, err := db.RunEventsSince(ctx, 0, intentID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load events: %v", err)), nil
	}

	// Reverse and take last 20.
	if len(events) > 20 {
		events = events[len(events)-20:]
	}

	return mcp.NewToolResultStructuredOnly(events), nil
}
