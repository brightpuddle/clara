package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/xdg"
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
// hidden      — omit from /help output.
// tuiOnly     — refuse to run in non-interactive CLI mode.
// subcommands — optional list of sub-args shown during tab completion.
type cmdEntry struct {
	name        string
	desc        string
	hidden      bool
	tuiOnly     bool
	subcommands []string
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
		name:    "edit",
		desc:    "Edit a today item in $EDITOR: edit <number>",
		tuiOnly: true,
		runCmd:  nil, // handled specially in model.go via editItemCmd
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
		name:        "server",
		desc:        "Server commands: server status",
		subcommands: []string{"status"},
		run:         runServer,
	},
	{
		name:        "agent",
		desc:        "Agent commands: agent status|start|stop",
		subcommands: []string{"start", "status", "stop"},
		run:         runAgent,
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

// subCandidatesFor returns subcommand names for cmd that have prefix as a prefix.
func subCandidatesFor(cmdName, prefix string) []string {
	cmd, err := matchCmd(cmdName)
	if err != nil || len(cmd.subcommands) == 0 {
		return nil
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	var out []string
	for _, sub := range cmd.subcommands {
		if strings.HasPrefix(sub, prefix) {
			out = append(out, sub)
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
	sb.WriteString("\n  /suggest              List pending backlink suggestions")
	sb.WriteString("\n  /approve <id>         Approve a suggestion by ID")
	sb.WriteString("\n  /reject <id>          Reject a suggestion by ID")
	sb.WriteString("\n  /find <query>         Search files with Spotlight and open")
	sb.WriteString("\n  /edit <n>             Edit today item n in $EDITOR")
	sb.WriteString("\n  /today                Return to today view")
	sb.WriteString("\n  /server status        Show server uptime and suggestion counts")
	sb.WriteString("\n  /agent status         Show local agent status")
	sb.WriteString("\n  /agent start          Start the local agent")
	sb.WriteString("\n  /agent stop           Stop the local agent")
	sb.WriteString("\n  /help                 Show this help")
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

// ---- edit command -----------------------------------------------------------

// editDoneMsg is sent when the user saves and exits $EDITOR after editing an item.
type editDoneMsg struct {
	original *ClaraItemJSON
	content  []byte // full YAML+md content after editing
	err      error
}

// editItemCmd opens the item in $EDITOR and returns an editDoneMsg on close.
func editItemCmd(item *ClaraItemJSON) tea.Cmd {
	// Render the item as YAML+md to a temp file.
	content := proposalExpandText(item) // reuse the renderer from model.go
	tmp, err := os.CreateTemp("", "clara-edit-*.md")
	if err != nil {
		e := err
		return func() tea.Msg { return editDoneMsg{err: e} }
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		e := err
		return func() tea.Msg { return editDoneMsg{err: e} }
	}
	tmp.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, tmp.Name())

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(tmp.Name())
		if err != nil {
			return editDoneMsg{original: item, err: err}
		}
		data, readErr := os.ReadFile(tmp.Name())
		if readErr != nil {
			return editDoneMsg{original: item, err: readErr}
		}
		return editDoneMsg{original: item, content: data}
	})
}

func runServer(ctx context.Context, args []string, api *APIClient) (string, error) {
	sub := "status"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "status":
		s, err := api.GetStatus(ctx)
		if err != nil {
			return "", fmt.Errorf("server unreachable: %w", err)
		}
		return fmt.Sprintf(
			"Server status: %s\nUptime:         %s\nSuggestions:    %d pending  %d approved  %d rejected",
			s.Status, s.Uptime,
			s.Suggestions.Pending, s.Suggestions.Approved, s.Suggestions.Rejected,
		), nil
	default:
		return "", fmt.Errorf("unknown server subcommand %q — try: status", sub)
	}
}

// ---- agent command ----------------------------------------------------------

func runAgent(ctx context.Context, args []string, _ *APIClient) (string, error) {
	sub := "status"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "status":
		return agentSocketStatus(ctx)
	case "stop":
		return agentSocketStop(ctx)
	case "start":
		return agentStart(ctx)
	default:
		return "", fmt.Errorf("unknown agent subcommand %q — try: status, start, stop", sub)
	}
}

// agentSocketPath resolves the agent Unix socket path.
func agentSocketPath() (string, error) {
	return xdg.RuntimeFile("agent.sock")
}

// dialAgent connects to the agent socket and returns a net.Conn (caller closes it).
func dialAgent() (net.Conn, error) {
	path, err := agentSocketPath()
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("agent not running (no socket at %s)", path)
	}
	return conn, nil
}

// sendAgentCmd sends a JSON command to the agent socket and returns the raw response.
func sendAgentCmd(ctx context.Context, cmd string) (map[string]any, error) {
	conn, err := dialAgent()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	req, _ := json.Marshal(map[string]string{"cmd": cmd})
	fmt.Fprintln(conn, string(req))

	var resp map[string]any
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return resp, nil
}

func agentSocketStatus(ctx context.Context) (string, error) {
	resp, err := sendAgentCmd(ctx, "status")
	if err != nil {
		return "Agent: not running", nil //nolint:nilerr
	}
	pid := int64(0)
	if v, ok := resp["pid"].(float64); ok {
		pid = int64(v)
	}
	uptime, _ := resp["uptime"].(string)
	notesDir, _ := resp["notes_dir"].(string)
	serverAddr, _ := resp["server_addr"].(string)
	applied := int64(0)
	if v, ok := resp["actions_applied"].(float64); ok {
		applied = int64(v)
	}
	return fmt.Sprintf(
		"Agent: running (pid %d)\nUptime:          %s\nNotes directory: %s\nServer:          %s\nActions applied: %d",
		pid, uptime, notesDir, serverAddr, applied,
	), nil
}

func agentSocketStop(ctx context.Context) (string, error) {
	resp, err := sendAgentCmd(ctx, "stop")
	if err != nil {
		return "", fmt.Errorf("agent stop: %w", err)
	}
	if ok, _ := resp["ok"].(bool); ok {
		return "Agent stopped.", nil
	}
	return "", fmt.Errorf("agent stop: unexpected response")
}

// agentBinaryPath returns the path to the clara-agent binary.
// It looks in the same directory as the running clara binary first.
func agentBinaryPath() string {
	self, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(self), "clara-agent")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Fall back to PATH.
	if path, err := exec.LookPath("clara-agent"); err == nil {
		return path
	}
	return "clara-agent"
}

func agentStart(ctx context.Context) (string, error) {
	// Check if already running.
	if _, err := dialAgent(); err == nil {
		return "Agent is already running.", nil
	}

	bin := agentBinaryPath()
	cmd := exec.Command(bin)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start clara-agent: %w", err)
	}

	// Wait up to 2s for the socket to appear.
	path, _ := agentSocketPath()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return fmt.Sprintf("Agent started (pid %d).", cmd.Process.Pid), nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Sprintf("Agent started (pid %d) — socket not yet ready.", cmd.Process.Pid), nil
}

