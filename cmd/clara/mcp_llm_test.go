package main

import "testing"

func TestMCPLLMCmdDefaults(t *testing.T) {
	if mcpserverLLMCmd.Use != "llm" {
		t.Fatalf("unexpected llm command use: %q", mcpserverLLMCmd.Use)
	}
}
