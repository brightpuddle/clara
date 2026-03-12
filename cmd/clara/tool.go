package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/spf13/cobra"
)

var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Manage tools",
}

var toolListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered tools (built-in and MCP servers)",
	Long: `List all tools registered in the running agent, including internal tools
(db.*, bridge.*) and tools from connected MCP servers.

Each tool is shown with its name and description.`,
	RunE:         runToolList,
	SilenceUsage: true,
}

var toolShowCmd = &cobra.Command{
	Use:   "show <server-or-tool>",
	Short: "Show capabilities of a server or tool",
	Long: `Show the full capabilities of an MCP server or the details of a specific tool.

Pass a server name to see all of its tools, resources, and prompts:
  clara tool show fs

Pass a qualified tool name to see details for a single tool:
  clara tool show fs.read_file`,
	Args:         cobra.ExactArgs(1),
	RunE:         runToolShow,
	SilenceUsage: true,
}

func init() {
	toolCmd.AddCommand(toolListCmd, toolShowCmd)
}

func runToolList(cmd *cobra.Command, args []string) error {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodToolList})
	if err != nil {
		return fmt.Errorf("tool list request failed: %w", err)
	}
	items, ok := resp.Data.([]any)
	if !ok {
		prettyPrint(resp.Data)
		return nil
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		desc, _ := m["description"].(string)
		if desc != "" {
			fmt.Printf("%-30s %s\n", name, desc)
		} else {
			fmt.Println(name)
		}
	}
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
	data, ok := resp.Data.(map[string]any)
	if !ok {
		prettyPrint(resp.Data)
		return nil
	}
	printCapabilities(data)
	return nil
}

// printCapabilities renders a tool/server capabilities map in a structured,
// human-readable format.
func printCapabilities(data map[string]any) {
	name, _ := data["name"].(string)
	desc, _ := data["description"].(string)

	fmt.Printf("Server: %s\n", name)
	if desc != "" {
		fmt.Printf("  %s\n", desc)
	}
	fmt.Println()

	printToolSection(data)
	printResourceSection(data)
	printPromptSection(data)
}

func printToolSection(data map[string]any) {
	tools, _ := data["tools"].([]any)
	if len(tools) == 0 {
		fmt.Println("Tools: none")
		fmt.Println()
		return
	}
	fmt.Printf("Tools (%d):\n", len(tools))
	for _, item := range tools {
		t, ok := item.(map[string]any)
		if !ok {
			continue
		}
		toolName, _ := t["name"].(string)
		toolDesc, _ := t["description"].(string)

		fmt.Printf("  %-30s %s\n", toolName, toolDesc)

		params, _ := t["parameters"].([]any)
		if len(params) == 0 {
			continue
		}

		// Sort params: required first, then alphabetically.
		type param struct {
			name     string
			typ      string
			desc     string
			required bool
		}
		var ps []param
		for _, p := range params {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			pName, _ := m["name"].(string)
			pType, _ := m["type"].(string)
			pDesc, _ := m["description"].(string)
			pReq, _ := m["required"].(bool)
			ps = append(ps, param{pName, pType, pDesc, pReq})
		}
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].required != ps[j].required {
				return ps[i].required
			}
			return ps[i].name < ps[j].name
		})
		for _, p := range ps {
			req := " "
			if p.required {
				req = "*"
			}
			typStr := p.typ
			if typStr == "" {
				typStr = "any"
			}
			fmt.Printf("    %s %-20s %-10s %s\n", req, p.name, "("+typStr+")", p.desc)
		}
	}
	fmt.Println()
}

func printResourceSection(data map[string]any) {
	resources, _ := data["resources"].([]any)
	if len(resources) == 0 {
		fmt.Println("Resources: none")
		fmt.Println()
		return
	}
	fmt.Printf("Resources (%d):\n", len(resources))
	for _, item := range resources {
		r, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rName, _ := r["name"].(string)
		rURI, _ := r["uri"].(string)
		rDesc, _ := r["description"].(string)
		rMime, _ := r["mime_type"].(string)

		fmt.Printf("  %-30s %s\n", rName, rDesc)
		parts := []string{}
		if rURI != "" {
			parts = append(parts, "uri: "+rURI)
		}
		if rMime != "" {
			parts = append(parts, "mime: "+rMime)
		}
		if len(parts) > 0 {
			fmt.Printf("    %s\n", strings.Join(parts, "  "))
		}
	}
	fmt.Println()
}

func printPromptSection(data map[string]any) {
	prompts, _ := data["prompts"].([]any)
	if len(prompts) == 0 {
		fmt.Println("Prompts: none")
		fmt.Println()
		return
	}
	fmt.Printf("Prompts (%d):\n", len(prompts))
	for _, item := range prompts {
		p, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pName, _ := p["name"].(string)
		pDesc, _ := p["description"].(string)
		fmt.Printf("  %-30s %s\n", pName, pDesc)
	}
	fmt.Println()
}
