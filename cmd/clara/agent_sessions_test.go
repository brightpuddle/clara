package main

import (
	"testing"

	"github.com/brightpuddle/clara/internal/config"
)

func TestAgentToolPrefix(t *testing.T) {
	old := cfg
	defer func() { cfg = old }()
	cfg = &config.Config{Agents: config.AgentsConfig{
		Host:        "eve",
		RemoteHosts: []config.AgentRemoteHost{{Name: "sophia", ToolPrefix: "laptop."}},
	}}
	for host, want := range map[string]string{"": "", "eve": "", "sophia": "laptop."} {
		got, err := agentToolPrefix(host)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v", host, got, err)
		}
	}
	if _, err := agentToolPrefix("mars"); err == nil {
		t.Error("want error for unknown host")
	}
}
