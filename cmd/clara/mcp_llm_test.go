package main

import "testing"

func TestMCPLLMCmdDefaults(t *testing.T) {
	if mcpLLMCmd.Use != "llm" {
		t.Fatalf("unexpected llm command use: %q", mcpLLMCmd.Use)
	}

	if got, want := mcpLLMDefaultProvider, "gemini"; got != want {
		t.Fatalf("default provider = %q, want %q", got, want)
	}
	if got, want := mcpLLMGeminiModel, "gemini-2.5-flash"; got != want {
		t.Fatalf("default gemini model = %q, want %q", got, want)
	}
}
