package main

import (
	"os/exec"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
)

type Shell struct{}

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
