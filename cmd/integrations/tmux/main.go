package main

import (
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
)

func main() {
	tmux, err := newTmux()
	if err != nil {
		// Serve a stub that returns the availability error from every call so
		// the daemon can start cleanly even when tmux is absent.
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: contract.HandshakeConfig,
			Plugins: map[string]plugin.Plugin{
				"tmux": &contract.TmuxIntegrationPlugin{Impl: &unavailableStub{err: err}},
			},
		})
		return
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"tmux": &contract.TmuxIntegrationPlugin{Impl: tmux},
		},
	})
}
