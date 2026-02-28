package tui

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// inputResultMsg is delivered to the model when a command finishes.
type inputResultMsg struct {
	output string
	err    error
}

// findReadyMsg carries the mdfind results; triggers fzf launch in the model.
type findReadyMsg struct {
	files []string
}

// cmdEntry describes a dispatchable command.
// Exactly one of run or runCmd must be set.
// hidden   — omit from /help output.
// tuiOnly  — refuse to run in non-interactive CLI mode.
type cmdEntry struct {
	name    string
	desc    string
	hidden  bool
	tuiOnly bool
	// Standard async handler: result appears as inputResultMsg.
	run func(ctx context.Context, args []string, api *APIClient) (string, error)
	// TUI handler: returns a tea.Cmd directly (for commands that need terminal control).
	runCmd func(args []string, api *APIClient) tea.Cmd
}

var registry = []cmdEntry{
	{
		name: "suggest",
		desc: "List pending backlink suggestions",
		run:  runSuggest,
	},
	{
		name: "approve",
		desc: "Approve a suggestion by ID: approve <id>",
		run:  runApprove,
	},
	{
		name: "reject",
		desc: "Reject a suggestion by ID: reject <id>",
		run:  runReject,
	},
	{
		name:    "find",
		desc:    "Search files with Spotlight and open: find <query>",
		tuiOnly: true,
		runCmd:  launchFind,
	},
	{
		name:    "today",
		desc:    "Show today view",
		tuiOnly: true,
		runCmd:  func(_ []string, _ *APIClient) tea.Cmd { return func() tea.Msg { return showTodayMsg{} } },
	},
	{
		name: "help",
		desc: "Show available commands",
		run:  runHelp,
	},
	{
		name:    "quit",
		hidden:  true,
		tuiOnly: true,
		runCmd:  func(_ []string, _ *APIClient) tea.Cmd { return tea.Quit },
	},
	{
		name:    "exit",
		hidden:  true,
		tuiOnly: true,
		runCmd:  func(_ []string, _ *APIClient) tea.Cmd { return tea.Quit },
	},
}

// matchCmd finds the unique command whose name starts with `name`.
func matchCmd(name string) (*cmdEntry, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	var hits []int
	for i, c := range registry {
		if strings.HasPrefix(c.name, name) {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("unknown command %q", name)
	case 1:
		return &registry[hits[0]], nil
	default:
		names := make([]string, len(hits))
		for i, idx := range hits {
			names[i] = "/" + registry[idx].name
		}
		sort.Strings(names)
		return nil, fmt.Errorf("ambiguous: %q matches %s", name, strings.Join(names, ", "))
	}
}

// candidatesFor returns visible command names that have prefix as a prefix.
func candidatesFor(prefix string) []string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	var out []string
	for _, c := range registry {
		if !c.hidden && strings.HasPrefix(c.name, prefix) {
			out = append(out, c.name)
		}
	}
	return out
}

// dispatch parses a line from the interactive prompt (leading / is optional).
func dispatch(line string, api *APIClient) tea.Cmd {
	line = strings.TrimPrefix(line, "/")
	name, args := parseLine(line)
	if name == "" {
		return nil
	}
	cmd, err := matchCmd(name)
	if err != nil {
		e := err
		return func() tea.Msg { return inputResultMsg{err: e} }
	}
	if cmd.runCmd != nil {
		return cmd.runCmd(args, api)
	}
	return func() tea.Msg {
		out, runErr := cmd.run(context.Background(), args, api)
		return inputResultMsg{output: out, err: runErr}
	}
}

// RunCommand executes a command synchronously; used for non-interactive CLI mode.
func RunCommand(line string, api *APIClient) (string, error) {
	name, args := parseLine(line)
	if name == "" {
		return "", nil
	}
	cmd, err := matchCmd(name)
	if err != nil {
		return "", err
	}
	if cmd.tuiOnly {
		return "", fmt.Errorf("command %q is only available in interactive mode", cmd.name)
	}
	return cmd.run(context.Background(), args, api)
}

func parseLine(line string) (name string, args []string) {
	line = strings.TrimSpace(line)
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

// ---- Command handlers -------------------------------------------------------

func runSuggest(ctx context.Context, _ []string, api *APIClient) (string, error) {
	items, err := api.ListSuggestions(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch suggestions: %w", err)
	}
	if len(items) == 0 {
		return "No pending suggestions.", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Pending suggestions (%d):\n", len(items))
	for _, s := range items {
		src := filepath.Base(s.SourcePath)
		src = strings.TrimSuffix(src, filepath.Ext(src))
		fmt.Fprintf(&sb, "\n  #%-4d %s  →  [[%s]]  %.0f%%",
			s.ID, src, s.TargetTitle, s.Similarity*100)
		if s.Context != "" {
			fmt.Fprintf(&sb, "\n        %s", truncate(s.Context, 80))
		}
	}
	return sb.String(), nil
}

func runApprove(ctx context.Context, args []string, api *APIClient) (string, error) {
	id, err := parseID(args)
	if err != nil {
		return "", err
	}
	if err := api.Approve(ctx, id); err != nil {
		return "", fmt.Errorf("approve #%d: %w", id, err)
	}
	return fmt.Sprintf("✓ Approved #%d — agent will apply the link.", id), nil
}

func runReject(ctx context.Context, args []string, api *APIClient) (string, error) {
	id, err := parseID(args)
	if err != nil {
		return "", err
	}
	if err := api.Reject(ctx, id); err != nil {
		return "", fmt.Errorf("reject #%d: %w", id, err)
	}
	return fmt.Sprintf("✗ Rejected #%d.", id), nil
}

func runHelp(_ context.Context, _ []string, _ *APIClient) (string, error) {
	var sb strings.Builder
	sb.WriteString("Available commands (prefix-matched, e.g. /s = /suggest):\n")
	sb.WriteString("\n  /suggest         List pending backlink suggestions")
	sb.WriteString("\n  /approve <id>    Approve a suggestion by ID")
	sb.WriteString("\n  /reject <id>     Reject a suggestion by ID")
	sb.WriteString("\n  /find <query>    Search files with Spotlight and open")
	sb.WriteString("\n  /today           Return to today view")
	sb.WriteString("\n  /help            Show this help")
	return sb.String(), nil
}

// launchFind runs mdfind then hands off to fzf via findReadyMsg.
func launchFind(args []string, _ *APIClient) tea.Cmd {
	query := strings.Join(args, " ")
	if query == "" {
		return func() tea.Msg {
			return inputResultMsg{err: fmt.Errorf("usage: find <spotlight query>")}
		}
	}
	return func() tea.Msg {
		out, err := exec.Command("mdfind", query).Output()
		if err != nil {
			return inputResultMsg{err: fmt.Errorf("mdfind: %w", err)}
		}
		files := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(files) == 0 || (len(files) == 1 && files[0] == "") {
			return inputResultMsg{output: "No files found."}
		}
		return findReadyMsg{files: files}
	}
}

func parseID(args []string) (int64, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("usage: <command> <id>")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id %q: expected a number", args[0])
	}
	return id, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

