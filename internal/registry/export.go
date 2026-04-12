package registry

import (
	"context"
	"fmt"
	"reflect"

	"github.com/mark3labs/mcp-go/mcp"
)

// ExportToolHandler wraps a Clara registry tool call in an mcp-go standard handler.
func ExportToolHandler(
	reg *Registry,
	toolName string,
) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := reg.Call(ctx, toolName, request.GetArguments())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return FormatToolResult(result)
	}
}

// FormatToolResult converts an arbitrary Go value from a Clara tool into an
// appropriate mcp.CallToolResult.
func FormatToolResult(result any) (*mcp.CallToolResult, error) {
	switch value := result.(type) {
	case nil:
		return mcp.NewToolResultText(""), nil
	case string:
		return mcp.NewToolResultText(value), nil
	case []byte:
		return mcp.NewToolResultText(string(value)), nil
	}

	kind := reflect.TypeOf(result).Kind()
	switch kind {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		return mcp.NewToolResultJSON(result)
	default:
		return mcp.NewToolResultText(fmt.Sprint(result)), nil
	}
}
