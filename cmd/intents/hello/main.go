package main

import (
	"fmt"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
)

type HelloIntent struct{}

func (i *HelloIntent) Execute(name string, shell contract.ShellIntegration) error {
	// a) Call the provided shell
	output, err := shell.Run("echo Hello " + name + " from shell")
	if err != nil {
		return err
	}
	fmt.Print(output)

	// b) Directly print
	fmt.Printf("Hello %s (direct)\n", name)
	return nil
}

func main() {
	intent := &HelloIntent{}

	var pluginMap = map[string]plugin.Plugin{
		"intent": &contract.IntentPlugin{Impl: intent},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins:         pluginMap,
		// No logger provided, so output will go to host's stderr
	})
}
