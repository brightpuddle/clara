package main

import (
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
)

func main() {
	impl := &LLMPlugin{}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"llm": &contract.LLMIntegrationPlugin{Impl: impl},
		},
	})
}
