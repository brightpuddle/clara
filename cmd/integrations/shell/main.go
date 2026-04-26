package main

import (
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
	"github.com/mark3labs/mcp-go/mcp"
)

type Shell struct{}

func (s *Shell) Configure(config []byte) error {
	return nil
}

func (s *Shell) Description() (string, error) {
	return "Built-in shell integration: run shell commands.", nil
}

func (s *Shell) Tools() ([]byte, error) {
	return json.Marshal([]mcp.Tool{
		mcp.NewTool(
			"run",
			mcp.WithDescription("Run a shell command"),
			mcp.WithString("command", mcp.Required(), mcp.Description("The shell command to run")),
		),
	})
}

type RunShellArgs struct {
	Command string `json:"command"`
}

func (s *Shell) CallTool(name string, args []byte) ([]byte, error) {
	if name == "run" {
		var parsed RunShellArgs
		if err := json.Unmarshal(args, &parsed); err != nil {
			return nil, err
		}

		out, err := s.Run(parsed.Command)
		if err != nil {
			return nil, err
		}

		return json.Marshal(map[string]string{"output": out})
	}
	return nil, fmt.Errorf("unknown tool: %s", name)
}

func (s *Shell) Run(command string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func main() {
	shell := &Shell{}

	var pluginMap = map[string]plugin.Plugin{
		"shell": &contract.ShellIntegrationPlugin{Impl: shell},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins:         pluginMap,
	})
}
