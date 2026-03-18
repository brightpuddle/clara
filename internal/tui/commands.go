package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/toolcatalog"
	tea "github.com/charmbracelet/bubbletea"
)

type CommandSpec struct {
	Path         string
	Summary      string
	Usage        string
	Autocomplete string
}

type commandResultMsg struct {
	output            string
	openNotifications bool
	quit              bool
}

type commandErrorMsg struct {
	output string
}

func commandSpecs() []CommandSpec {
	return []CommandSpec{
		{Path: "/help", Summary: "Show available slash commands", Usage: "/help"},
		{Path: "/quit", Summary: "Quit the TUI", Usage: "/quit"},
		{Path: "/exit", Summary: "Quit the TUI", Usage: "/exit"},
		{
			Path:    "/intent run",
			Summary: "Run a one-off intent file (not available in TUI)",
			Usage:   "/intent run <intent-file>",
		},
		{Path: "/agent start", Summary: "Show how to start the Clara agent", Usage: "/agent start"},
		{Path: "/agent status", Summary: "Show agent status", Usage: "/agent status"},
		{Path: "/agent stop", Summary: "Stop the running agent", Usage: "/agent stop"},
		{Path: "/intent list", Summary: "List active intents", Usage: "/intent list"},
		{
			Path:    "/intent trigger",
			Summary: "Trigger an installed intent by id",
			Usage:   "/intent trigger <id>",
		},
		{
			Path:    "/mcp fs",
			Summary: "Start the filesystem MCP server (not available in TUI)",
			Usage:   "/mcp fs",
		},
		{
			Path:    "/mcp db",
			Summary: "Start the SQLite MCP server (not available in TUI)",
			Usage:   "/mcp db [path]",
		},
		{
			Path:    "/mcp taskwarrior",
			Summary: "Start the Taskwarrior MCP server (not available in TUI)",
			Usage:   "/mcp taskwarrior",
		},
		{Path: "/tool list", Summary: "List registered tools", Usage: "/tool list [prefix]"},
		{Path: "/tool show", Summary: "Show one tool schema", Usage: "/tool show <name>"},
		{
			Path:    "/tool call",
			Summary: "Call a tool directly",
			Usage:   "/tool call <name> [key=value ...]",
		},
		{Path: "/notifications", Summary: "Open the notifications view", Usage: "/notifications"},
	}
}

func executeSlashCommand(client *IPCClient, theme Theme, line string, specs []CommandSpec) tea.Cmd {
	return func() tea.Msg {
		result, err := runSlashCommand(client, theme, line, specs)
		if err != nil {
			return commandErrorMsg{output: err.Error()}
		}
		return commandResultMsg(result)
	}
}

type slashCommandResult struct {
	output            string
	openNotifications bool
	quit              bool
}

func runSlashCommand(
	client *IPCClient,
	theme Theme,
	line string,
	specs []CommandSpec,
) (slashCommandResult, error) {
	tokens, err := splitCommandLine(strings.TrimSpace(line))
	if err != nil {
		return slashCommandResult{}, err
	}
	if len(tokens) == 0 {
		return slashCommandResult{}, nil
	}

	switch {
	case tokens[0] == "/help":
		return slashCommandResult{output: renderHelp(theme, specs)}, nil
	case tokens[0] == "/quit" || tokens[0] == "/exit":
		return slashCommandResult{quit: true}, nil
	case len(tokens) >= 2 && tokens[0] == "/intent" && tokens[1] == "run":
		return slashCommandResult{
			output: "The interactive TUI does not support /intent run yet. Use `clara intent run <intent-file>` from the shell.",
		}, nil
	case len(tokens) >= 2 && tokens[0] == "/agent" && tokens[1] == "start":
		if client.IsRunning() {
			return slashCommandResult{output: "Clara agent is already running."}, nil
		}
		return slashCommandResult{output: "Start the agent with: clara serve"}, nil
	case len(tokens) >= 2 && tokens[0] == "/mcp":
		return slashCommandResult{
			output: "Built-in MCP launcher commands are shell-only. Run `clara mcp ...` outside the TUI.",
		}, nil
	case len(tokens) >= 2 && tokens[0] == "/agent" && tokens[1] == "status":
		if !client.IsRunning() {
			return slashCommandResult{output: "Clara agent is not running."}, nil
		}
		counts, err := client.StatusCounts()
		if err != nil {
			return slashCommandResult{}, err
		}
		return slashCommandResult{output: formatStatusCounts(counts)}, nil
	case len(tokens) >= 2 && tokens[0] == "/agent" && tokens[1] == "stop":
		resp, err := client.Request(ipc.MethodShutdown, nil)
		if err != nil {
			return slashCommandResult{}, err
		}
		return slashCommandResult{output: resp.Message}, nil
	case len(tokens) >= 2 && tokens[0] == "/intent" && tokens[1] == "list":
		intents, err := client.ListIntents()
		if err != nil {
			return slashCommandResult{}, err
		}
		return slashCommandResult{output: formatIntentList(intents)}, nil
	case len(tokens) >= 3 && tokens[0] == "/intent" && tokens[1] == "trigger":
		resp, err := client.Request(ipc.MethodRun, map[string]any{"id": tokens[2]})
		if err != nil {
			return slashCommandResult{}, err
		}
		return slashCommandResult{output: resp.Message}, nil
	case len(tokens) >= 2 && tokens[0] == "/tool" && tokens[1] == "list":
		if len(tokens) >= 3 {
			tools, err := client.ListTools(tokens[2])
			if err != nil {
				return slashCommandResult{}, err
			}
			return slashCommandResult{output: toolcatalog.FormatToolList(tools, true)}, nil
		}
		providers, err := client.ListProviders()
		if err != nil {
			return slashCommandResult{}, err
		}
		return slashCommandResult{output: toolcatalog.FormatProviderList(providers, true)}, nil
	case len(tokens) >= 3 && tokens[0] == "/tool" && tokens[1] == "show":
		tool, err := client.ShowTool(tokens[2])
		if err != nil {
			return slashCommandResult{}, err
		}
		return slashCommandResult{output: toolcatalog.FormatToolDetails(tool, true)}, nil
	case len(tokens) >= 3 && tokens[0] == "/tool" && tokens[1] == "call":
		if len(tokens) == 3 && !strings.Contains(tokens[2], ".") {
			tools, err := client.ListTools(tokens[2])
			if err != nil {
				return slashCommandResult{}, err
			}
			return slashCommandResult{output: toolcatalog.FormatToolList(tools, true)}, nil
		}
		args, err := parseKeyValueArgs(tokens[3:])
		if err != nil {
			return slashCommandResult{}, err
		}
		resp, err := client.DoRaw(
			ipc.Request{
				Method: ipc.MethodToolCall,
				Params: map[string]any{"name": tokens[2], "args": args},
			},
		)
		if err != nil {
			return slashCommandResult{
				output: RenderJSON(
					theme,
					map[string]any{"error": fmt.Sprintf("tool call request failed: %v", err)},
				),
			}, nil
		}
		if resp.Error != "" {
			return slashCommandResult{
				output: RenderJSON(theme, map[string]any{"error": resp.Error}),
			}, nil
		}
		return slashCommandResult{output: RenderJSON(theme, resp.Data)}, nil
	case tokens[0] == "/notifications":
		return slashCommandResult{openNotifications: true}, nil
	default:
		return slashCommandResult{}, fmt.Errorf("unknown command: %s", line)
	}
}

func renderHelp(theme Theme, specs []CommandSpec) string {
	var b strings.Builder
	b.WriteString("Available commands:\n")
	for _, spec := range specs {
		b.WriteString("  ")
		b.WriteString(theme.Cyan(spec.Usage))
		b.WriteString("\n")
		b.WriteString("    ")
		b.WriteString(theme.Dimmed(spec.Summary))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatStatusCounts(counts StatusCounts) string {
	return fmt.Sprintf(
		"Clara agent is running.\nservers: %d\ntools: %d\nintents: %d",
		counts.Servers,
		counts.Tools,
		counts.Intents,
	)
}

func formatIntentList(intents []IntentSummary) string {
	if len(intents) == 0 {
		return "No active intents."
	}
	lines := make([]string, 0, len(intents))
	for _, intent := range intents {
		lines = append(lines, intent.ID)
	}
	return strings.Join(lines, "\n")
}

func splitCommandLine(input string) ([]string, error) {
	var tokens []string
	var buf strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if buf.Len() > 0 {
			tokens = append(tokens, buf.String())
			buf.Reset()
		}
	}

	for _, r := range input {
		switch {
		case escaped:
			buf.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				buf.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			buf.WriteRune(r)
		}
	}
	if escaped {
		return nil, fmt.Errorf("unfinished escape sequence")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	flush()
	return tokens, nil
}

func parseKeyValueArgs(tokens []string) (map[string]any, error) {
	args := make(map[string]any, len(tokens))
	for _, token := range tokens {
		key, value, ok := strings.Cut(token, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid argument %q: expected key=value", token)
		}
		if _, exists := args[key]; exists {
			return nil, fmt.Errorf("duplicate argument %q", key)
		}
		parsed, err := parseToolValue(value)
		if err != nil {
			return nil, fmt.Errorf("parse argument %q: %w", key, err)
		}
		args[key] = parsed
	}
	return args, nil
}

func parseToolValue(raw string) (any, error) {
	if raw == "" {
		return "", nil
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err == nil {
		return value, nil
	}
	return raw, nil
}
