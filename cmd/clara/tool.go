package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/toolcatalog"
	"github.com/spf13/cobra"
)

type toolParam = toolcatalog.Param
type toolDetails = toolcatalog.Tool
type providerSummary = toolcatalog.Provider

var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Manage tools",
}

var toolListCmd = &cobra.Command{
	Use:     "list [server]",
	Aliases: []string{"ls"},
	Short:   "List registered tools with their MCP-style signatures",
	Long: `List registered tools from internal Clara capabilities, the Swift bridge,
and connected MCP servers.

Pass an optional server prefix to filter the output:
  clara tool list db
  clara tool ls fs`,
	Args:          cobra.RangeArgs(0, 1),
	RunE:          runToolList,
	SilenceUsage:  true,
	SilenceErrors: false,
}

var toolShowCmd = &cobra.Command{
	Use:   "show <tool_name>",
	Short: "Show the full spec for a single tool",
	Long: `Show a tool's description and parameter schema in a readable MCP-style format.

Example:
  clara tool show fs.list_directory`,
	Args:         cobra.ExactArgs(1),
	RunE:         runToolShow,
	SilenceUsage: true,
}

var toolCallCmd = &cobra.Command{
	Use:   "call <tool_name> [key=value ...]",
	Short: "Call a tool directly and print its JSON result",
	Long: `Call a Clara tool directly through the running agent.

Arguments are passed as key=value pairs. Values are parsed as JSON when
possible, otherwise they are treated as strings.

Examples:
  clara tool call fs.list_directory path=.
  clara tool call db.query sql='SELECT 1 as n'
  clara tool call db.query sql='SELECT ? as n' params='[1]'`,
	Args:         cobra.MinimumNArgs(1),
	RunE:         runToolCall,
	SilenceUsage: true,
}

func init() {
	toolCmd.AddCommand(toolListCmd, toolShowCmd, toolCallCmd)
}

func runToolList(cmd *cobra.Command, args []string) error {
	req := ipc.Request{Method: ipc.MethodToolList}
	if len(args) == 1 {
		req.Params = map[string]any{"filter": args[0]}
	}

	resp, err := sendRequest(cfg.ControlSocketPath(), req)
	if err != nil {
		return fmt.Errorf("tool list request failed: %w", err)
	}

	if len(args) == 0 {
		providers, err := decodeProviderList(resp.Data)
		if err != nil {
			return err
		}
		printProviderList(providers)
		return nil
	}

	tools, err := decodeToolList(resp.Data)
	if err != nil {
		return err
	}
	printToolList(tools)
	return nil
}

func runToolShow(cmd *cobra.Command, args []string) error {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodToolShow,
		Params: map[string]any{"name": args[0]},
	})
	if err != nil {
		return fmt.Errorf("tool show request failed: %w", err)
	}

	tool, err := decodeTool(resp.Data)
	if err != nil {
		return err
	}

	printToolDetails(tool)
	return nil
}

func runToolCall(cmd *cobra.Command, args []string) error {
	parsedArgs, err := parseToolCallArgs(args[1:])
	if err != nil {
		prettyPrint(map[string]any{"error": err.Error()})
		return nil
	}

	resp, err := sendRawRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodToolCall,
		Params: map[string]any{
			"name": args[0],
			"args": parsedArgs,
		},
	})
	if err != nil {
		prettyPrint(map[string]any{"error": fmt.Sprintf("tool call request failed: %v", err)})
		return nil
	}
	if resp.Error != "" {
		prettyPrint(map[string]any{"error": resp.Error})
		return nil
	}
	if resp.Data == nil {
		fmt.Println("null")
		return nil
	}

	prettyPrint(resp.Data)
	return nil
}

func parseToolCallArgs(pairs []string) (map[string]any, error) {
	args := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		key, rawValue, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid argument %q: expected key=value", pair)
		}
		if _, exists := args[key]; exists {
			return nil, fmt.Errorf("duplicate argument %q", key)
		}

		value, err := parseToolCallValue(rawValue)
		if err != nil {
			return nil, fmt.Errorf("parse argument %q: %w", key, err)
		}
		args[key] = value
	}
	return args, nil
}

func parseToolCallValue(raw string) (any, error) {
	if raw == "" {
		return "", nil
	}

	var value any
	if err := json.Unmarshal([]byte(raw), &value); err == nil {
		return value, nil
	}
	return raw, nil
}

func decodeToolList(data any) ([]toolDetails, error) {
	if data == nil {
		return nil, nil
	}

	items, ok := data.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected tool list payload: %T", data)
	}

	tools := make([]toolDetails, 0, len(items))
	for _, item := range items {
		tool, err := decodeTool(item)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func decodeTool(data any) (toolDetails, error) {
	m, ok := data.(map[string]any)
	if !ok {
		return toolDetails{}, fmt.Errorf("unexpected tool payload: %T", data)
	}

	tool := toolDetails{
		Name:        stringValue(m["name"]),
		Description: stringValue(m["description"]),
		Examples:    stringSliceValue(m["examples"]),
	}

	params, err := decodeToolParams(m["parameters"])
	if err != nil {
		return toolDetails{}, err
	}
	tool.Parameters = params
	return tool, nil
}

func decodeToolParams(data any) ([]toolParam, error) {
	if data == nil {
		return nil, nil
	}

	items, ok := data.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected parameter payload: %T", data)
	}

	params := make([]toolParam, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected parameter entry: %T", item)
		}
		params = append(params, toolParam{
			Name:        stringValue(m["name"]),
			Type:        stringValue(m["type"]),
			Description: stringValue(m["description"]),
			Required:    boolValue(m["required"]),
		})
	}

	sort.Slice(params, func(i, j int) bool {
		if params[i].Required != params[j].Required {
			return params[i].Required
		}
		return params[i].Name < params[j].Name
	})
	return params, nil
}

func decodeProviderList(data any) ([]providerSummary, error) {
	if data == nil {
		return nil, nil
	}
	items, ok := data.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected provider list payload: %T", data)
	}
	providers := make([]providerSummary, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected provider payload: %T", item)
		}
		providers = append(providers, providerSummary{
			Name:        stringValue(m["name"]),
			Description: stringValue(m["description"]),
		})
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	return providers, nil
}

func printProviderList(providers []providerSummary) {
	if len(providers) == 0 {
		return
	}
	fmt.Println(toolcatalog.FormatProviderList(providers, useToolColors()))
}

func printToolList(tools []toolDetails) {
	if len(tools) == 0 {
		return
	}
	fmt.Println(toolcatalog.FormatToolList(tools, useToolColors()))
}

func printToolDetails(tool toolDetails) {
	fmt.Println(toolcatalog.FormatToolDetails(tool, useToolColors()))
}

func useToolColors() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminalFile(os.Stdout)
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func boolValue(value any) bool {
	b, _ := value.(bool)
	return b
}

func stringSliceValue(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
