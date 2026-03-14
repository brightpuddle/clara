package tui

import (
	"strings"
	"testing"
)

func TestSplitCommandLine(t *testing.T) {
	tokens, err := splitCommandLine(`/tool call db.query sql='SELECT 1 as n' params='[1]'`)
	if err != nil {
		t.Fatalf("splitCommandLine returned error: %v", err)
	}
	want := []string{"/tool", "call", "db.query", "sql=SELECT 1 as n", "params=[1]"}
	if len(tokens) != len(want) {
		t.Fatalf("got %v, want %v", tokens, want)
	}
	for i := range want {
		if tokens[i] != want[i] {
			t.Fatalf("token %d = %q, want %q", i, tokens[i], want[i])
		}
	}
}

func TestCompletionTokens(t *testing.T) {
	tokens, current, trailing := completionTokens("/agent st")
	if trailing || current != "st" || len(tokens) != 1 || tokens[0] != "agent" {
		t.Fatalf(
			"unexpected completion tokens: tokens=%v current=%q trailing=%v",
			tokens,
			current,
			trailing,
		)
	}

	tokens, current, trailing = completionTokens("/agent ")
	if !trailing || current != "" || len(tokens) != 1 || tokens[0] != "agent" {
		t.Fatalf(
			"unexpected trailing completion tokens: tokens=%v current=%q trailing=%v",
			tokens,
			current,
			trailing,
		)
	}
}

func TestCommandWordSuggestionsOnlyReturnsNextWord(t *testing.T) {
	items := commandWordSuggestions(commandSpecs(), nil, "ag")
	if len(items) != 1 || items[0].Display != "agent" || items[0].Insert != "/agent " {
		t.Fatalf("unexpected root suggestions: %+v", items)
	}

	items = commandWordSuggestions(commandSpecs(), []string{"agent"}, "st")
	if len(items) != 3 {
		t.Fatalf("unexpected nested suggestions: %+v", items)
	}
}

func TestDimmedHexFromRGB(t *testing.T) {
	light := dimmedHexFromRGB(255, 255, 255, false)
	if light != "#999999" {
		t.Fatalf("light dimmed = %q, want %q", light, "#999999")
	}
	dark := dimmedHexFromRGB(255, 255, 255, true)
	if dark != "#b7b7b7" {
		t.Fatalf("dark dimmed = %q, want %q", dark, "#b7b7b7")
	}
}

func TestRenderJSONColorsKeysAndStrings(t *testing.T) {
	theme := Theme{
		DimmedTrueColor: "#999999",
		MagentaCode:     35,
		CyanCode:        36,
		BlueCode:        34,
		GreenCode:       32,
	}
	out := RenderJSON(theme, map[string]any{"hello": "world", "count": float64(2)})
	if !strings.Contains(out, "\x1b[34m\"hello\"\x1b[0m") {
		t.Fatalf("expected blue key coloring, got %q", out)
	}
	if !strings.Contains(out, "\x1b[32m\"world\"\x1b[0m") {
		t.Fatalf("expected green string coloring, got %q", out)
	}
	if !strings.Contains(out, "2") {
		t.Fatalf("expected numeric value in output, got %q", out)
	}
}

func TestNotificationResponseSelection(t *testing.T) {
	service := NewNotificationService(nil)
	respCh := make(chan NotificationResponse, 1)
	service.pending["n1"] = respCh
	if !service.Respond("n1", "ack") {
		t.Fatal("expected respond to succeed")
	}
	resp := <-respCh
	if resp.ActionID != "ack" || resp.Status != "responded" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
