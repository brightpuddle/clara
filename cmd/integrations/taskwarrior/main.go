package main

import (
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
)

func main() {
	tw, err := newTaskwarrior()
	if err != nil {
		// Serve a stub that surfaces the availability error from every call so
		// the daemon can start cleanly even when `task` is absent.
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: contract.HandshakeConfig,
			Plugins: map[string]plugin.Plugin{
				"taskwarrior": &contract.TaskwarriorIntegrationPlugin{
					Impl: &unavailableStub{err: err},
				},
			},
		})
		return
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"taskwarrior": &contract.TaskwarriorIntegrationPlugin{Impl: tw},
		},
	})
}
