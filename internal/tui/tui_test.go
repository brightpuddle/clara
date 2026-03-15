package tui

import (
	"path/filepath"
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

type stubAutocompleteClient struct {
	providers []ProviderSummary
	tools     []ToolInfo
}

func (s stubAutocompleteClient) ListIntents() ([]IntentSummary, error) { return nil, nil }
func (s stubAutocompleteClient) ListProviders() ([]ProviderSummary, error) {
	return s.providers, nil
}
func (s stubAutocompleteClient) ListTools(filter string) ([]ToolInfo, error) {
	var tools []ToolInfo
	for _, tool := range s.tools {
		if filter == "" || strings.HasPrefix(tool.Name, filter) {
			tools = append(tools, tool)
		}
	}
	return tools, nil
}
func (s stubAutocompleteClient) ShowTool(name string) (ToolInfo, error) {
	for _, tool := range s.tools {
		if tool.Name == name {
			return tool, nil
		}
	}
	return ToolInfo{}, nil
}

func TestCompleteToolCallSuggestsProvidersFirst(t *testing.T) {
	client := stubAutocompleteClient{
		providers: []ProviderSummary{{Name: "fs"}, {Name: "db"}},
		tools: []ToolInfo{
			{Name: "fs.list_directory"},
			{Name: "fs.write_file"},
			{Name: "db.query"},
		},
	}

	items := completeInput(client, "/tool call f", commandSpecs())
	if len(items) != 1 || items[0].Display != "fs" || items[0].Insert != "/tool call fs" {
		t.Fatalf("unexpected provider suggestions: %#v", items)
	}

	items = completeInput(client, "/tool call fs.", commandSpecs())
	if len(items) != 2 {
		t.Fatalf("unexpected tool suffix suggestions: %#v", items)
	}
	if items[0].Display != "list_directory" || items[1].Display != "write_file" {
		t.Fatalf("unexpected tool suffix display: %#v", items)
	}
}

func TestCommandHistoryPersistsAndNavigates(t *testing.T) {
	history, err := loadCommandHistory(filepath.Join(t.TempDir(), "history.json"), 2)
	if err != nil {
		t.Fatalf("loadCommandHistory: %v", err)
	}

	if err := history.Add("/tool list"); err != nil {
		t.Fatalf("history add 1: %v", err)
	}
	if err := history.Add("/tool list"); err != nil {
		t.Fatalf("history add duplicate: %v", err)
	}
	if err := history.Add("/agent status"); err != nil {
		t.Fatalf("history add 2: %v", err)
	}
	if err := history.Add("/intent list"); err != nil {
		t.Fatalf("history add 3: %v", err)
	}

	reloaded, err := loadCommandHistory(history.path, 2)
	if err != nil {
		t.Fatalf("reload history: %v", err)
	}
	if len(reloaded.items) != 2 || reloaded.items[0] != "/agent status" || reloaded.items[1] != "/intent list" {
		t.Fatalf("unexpected persisted history: %#v", reloaded.items)
	}

	if got := reloaded.Previous(""); got != "/intent list" {
		t.Fatalf("Previous latest = %q", got)
	}
	if got := reloaded.Previous(""); got != "/agent status" {
		t.Fatalf("Previous older = %q", got)
	}
	if got := reloaded.Next(); got != "/intent list" {
		t.Fatalf("Next newer = %q", got)
	}
	if got := reloaded.Next(); got != "" {
		t.Fatalf("Next draft = %q", got)
	}
}
