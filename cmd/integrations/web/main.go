package main

import (
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
)

func main() {
	web := &Web{}

	var pluginMap = map[string]plugin.Plugin{
		"web": &contract.WebIntegrationPlugin{Impl: web},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins:         pluginMap,
	})
}
