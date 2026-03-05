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

// noteReadyMsg carries the note file list; triggers fzf+display/editor launch in the model.
type noteReadyMsg struct {
	files    []string
	newFile  string // non-empty when 'note new' was requested (pre-created path)
	editMode bool   // if true, fzf selection opens $EDITOR; otherwise displays in viewport
}

// taskItem is a parsed Apple Reminder/Task.
type taskItem struct {
	ExternalID  string `json:"externalId"`
	IsCompleted bool   `json:"isCompleted"`
	List        string `json:"list"`
	Priority    int    `json:"priority"`
	Title       string `json:"title"`
	DueDate     string `json:"dueDate,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// taskReadyMsg carries the task list; triggers fzf launch in the model.
type taskReadyMsg struct {
	items    []taskItem
	editMode bool // if true, fzf selection opens $EDITOR instead of viewport display
}

// taskSelectedMsg is sent after fzf selects a task (for display in viewport).
type taskSelectedMsg struct {
	item taskItem
}

// taskEditSelectedMsg is sent after fzf selects a task for $EDITOR editing.
type taskEditSelectedMsg struct {
	item taskItem
}

// cmdEntry describes a dispatchable command.
// Exactly one of run or runCmd must be set.
// hidden      — omit from help output.
// tuiOnly     — refuse to run in non-interactive CLI mode.
// subcommands — optional list of sub-args shown during tab completion.
// detail      — extended help shown by 'help <command>'.
type cmdEntry struct {
	name        string
	desc        string
	detail      string
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
		name:   "suggest",
		desc:   "List pending backlink suggestions",
		detail: "Fetches pending backlink suggestions from the server and displays them with their ID, source, target, and similarity score.\n\nUsage: suggest\n\nSee also: approve, reject",
		run:    runSuggest,
	},
	{
		name:   "approve",
		desc:   "Approve a suggestion by ID",
		detail: "Approves a backlink suggestion by its numeric ID. The agent will apply the link to the source document on its next poll.\n\nUsage: approve <id>\nExample: approve 42",
		run:    runApprove,
	},
	{
		name:   "reject",
		desc:   "Reject a suggestion by ID",
		detail: "Rejects a backlink suggestion by its numeric ID. The suggestion will not be shown again.\n\nUsage: reject <id>\nExample: reject 42",
		run:    runReject,
	},
	{
		name:    "find",
		desc:    "Search files with Spotlight and open",
		detail:  "Runs mdfind with your query, presents results in fzf, and opens the selected file with the default macOS app (open).\n\nUsage: find <spotlight query>\nExample: find project alpha meeting",
		tuiOnly: true,
		runCmd:  launchFind,
	},
	{
		name:        "note",
		desc:        "Browse notes (display in viewport; 'note edit' opens $EDITOR)",
		detail:      "Finds all markdown notes in your notes directory, presents them in fzf with a preview, and displays the selected note in the viewport.\n\nSubcommands:\n  note            Browse and display a note\n  note edit       Browse and open a note in $EDITOR\n  note new [slug] Create a new timestamped note in $EDITOR\n\nThe notes directory is read from agent.yaml (notes.dir), CLARA_NOTES_DIR env, or ~/notes.",
		tuiOnly:     true,
		subcommands: []string{"edit", "new"},
		runCmd:      launchNote,
	},
	{
		name:        "task",
		desc:        "Browse Apple Reminders/tasks",
		detail:      "Fetches pending tasks from Apple Reminders via reminders-cli, presents them in fzf, and displays the selected task in the viewport.\n\nSubcommands:\n  task              Browse and display a task\n  task edit         Browse and open a task in $EDITOR (with YAML schema)\n  task complete <list> <index>  Mark a task complete\n  task new <list> <text>        Add a new task\n  task lists        Show all reminder lists",
		tuiOnly:     true,
		subcommands: []string{"complete", "edit", "lists", "new"},
		runCmd:      launchTask,
	},
	{
		name:    "edit",
		desc:    "Edit a today item in $EDITOR",
		detail:  "Opens a today-view item by number in $EDITOR as YAML+markdown. On save and exit, the status field is parsed and approve/dismiss is applied if changed.\n\nUsage: edit <number>\nExample: edit 1",
		tuiOnly: true,
		runCmd:  nil, // handled specially in model.go via editItemCmd
	},
	{
		name:   "help",
		desc:   "Show available commands",
		detail: "Shows all available commands with descriptions.\n\nUsage: help [command]\nExample: help suggest",
		// run: assigned in init() to break the initialization cycle
	},
	{
		name:        "server",
		desc:        "Server commands",
		detail:      "Queries the clara-server for status information.\n\nSubcommands:\n  server status   Show uptime, ingested document count, and suggestion counts",
		subcommands: []string{"status"},
		run:         runServer,
	},
	{
		name:        "agent",
		desc:        "Local agent commands",
		detail:      "Controls the local clara-agent process via its Unix socket.\n\nSubcommands:\n  agent status   Show agent uptime, notes dir, files ingested, actions applied\n  agent start    Start the local agent in the background\n  agent stop     Stop the running local agent",
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
func init() {
	// Break the initialization cycle: assign help's run handler after registry is initialized.
	for i := range registry {
		if registry[i].name == "help" {
			registry[i].run = runHelp
			break
		}
	}
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
			names[i] = registry[idx].name
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
	if err != nil {
		return nil
	}
	// 'help' tab-completes to all visible command names.
	if cmd.name == "help" {
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		var out []string
		for _, c := range registry {
			if !c.hidden && strings.HasPrefix(c.name, prefix) {
				out = append(out, c.name)
			}
		}
		return out
	}
	if len(cmd.subcommands) == 0 {
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

func runHelp(_ context.Context, args []string, _ *APIClient) (string, error) {
	// 'help <command>' — show detailed help for one command.
	if len(args) > 0 {
		cmd, err := matchCmd(strings.TrimPrefix(args[0], "/"))
		if err != nil {
			return "", err
		}
		detail := cmd.detail
		if detail == "" {
			detail = cmd.desc
		}
		return fmt.Sprintf("%s — %s\n\n%s", cmd.name, cmd.desc, detail), nil
	}

	// Generic listing.
	var sb strings.Builder
	sb.WriteString("Available commands (prefix-matched, e.g. 'su' = 'suggest'):\n")
	for _, c := range registry {
		if c.hidden {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n  %-22s %s", c.name, c.desc))
	}
	sb.WriteString("\n\nType 'help <command>' for details, e.g. 'help note'")
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

// ---- note command ----------------------------------------------------------

// launchNote discovers notes files and hands off to fzf via noteReadyMsg.
// bare note → display in viewport; note edit → open in $EDITOR.
func launchNote(args []string, _ *APIClient) tea.Cmd {
	editMode := false

	// note edit — browse and open in $EDITOR.
	if len(args) > 0 && strings.HasPrefix("edit", args[0]) {
		editMode = true
		args = args[1:]
	}

	// note new — create a timestamped file and open immediately.
	if len(args) > 0 && strings.HasPrefix("new", args[0]) {
		return func() tea.Msg {
			cfg := readLocalConfig()
			ts := time.Now().Format("2006-01-02")
			name := ts + ".md"
			if len(args) > 1 {
				slug := strings.Join(args[1:], "-")
				slug = strings.Map(func(r rune) rune {
					if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
						(r >= '0' && r <= '9') || r == '-' || r == '_' {
						return r
					}
					if r == ' ' {
						return '-'
					}
					return -1
				}, slug)
				name = ts + "-" + slug + ".md"
			}
			path := filepath.Join(cfg.NotesDir, name)
			return noteReadyMsg{newFile: path}
		}
	}

	// note [query] — find files, launch fzf.
	return func() tea.Msg {
		cfg := readLocalConfig()
		// -L follows symlinks (notes dir may itself be a symlink or contain symlinked subdirs).
		findArgs := []string{"-L", cfg.NotesDir,
			"(", "-name", "*.md", "-o", "-name", "*.markdown", ")",
			"-not", "-path", "*/.git/*",
		}
		out, err := exec.Command("find", findArgs...).Output()
		if err != nil {
			return inputResultMsg{err: fmt.Errorf("find notes: %w", err)}
		}
		files := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(files) == 0 || (len(files) == 1 && files[0] == "") {
			return inputResultMsg{output: "No notes found in " + cfg.NotesDir}
		}
		sort.Strings(files)
		return noteReadyMsg{files: files, editMode: editMode}
	}
}

// ---- task command ------------------------------------------------------

// launchTask fetches tasks (Apple Reminders) and hands off via taskReadyMsg.
func launchTask(args []string, _ *APIClient) tea.Cmd {
	if len(args) > 0 {
		sub := args[0]
		switch {
		case strings.HasPrefix("edit", sub):
			return launchTaskEdit(args[1:])
		case strings.HasPrefix("complete", sub):
			return taskComplete(args[1:])
		case strings.HasPrefix("new", sub):
			return taskNew(args[1:])
		case strings.HasPrefix("lists", sub):
			return func() tea.Msg {
				out, err := exec.Command("reminders", "show-lists").Output()
				if err != nil {
					return inputResultMsg{err: fmt.Errorf("show-lists: %w", err)}
				}
				return inputResultMsg{output: "Reminder lists:\n" + strings.TrimSpace(string(out))}
			}
		}
	}

	// task [list] — fetch all (or specific list) and launch fzf for display.
	listArg := strings.Join(args, " ")
	return func() tea.Msg {
		var out []byte
		var err error
		if listArg == "" {
			out, err = exec.Command("reminders", "show-all", "--format", "json").Output()
		} else {
			out, err = exec.Command("reminders", "show", listArg, "--format", "json").Output()
		}
		if err != nil {
			return inputResultMsg{err: fmt.Errorf("reminders: %w", err)}
		}
		var items []taskItem
		if jsonErr := json.Unmarshal(out, &items); jsonErr != nil {
			return inputResultMsg{err: fmt.Errorf("parse tasks: %w", jsonErr)}
		}
		// Filter to incomplete only by default.
		var pending []taskItem
		for _, it := range items {
			if !it.IsCompleted {
				pending = append(pending, it)
			}
		}
		if len(pending) == 0 {
			return inputResultMsg{output: "No pending tasks."}
		}
		return taskReadyMsg{items: pending}
	}
}

// taskComplete runs `reminders complete <list> <index>`.
func taskComplete(args []string) tea.Cmd {
	if len(args) < 2 {
		return func() tea.Msg {
			return inputResultMsg{err: fmt.Errorf("usage: task complete <list> <index>")}
		}
	}
	list := args[0]
	idx := args[1]
	return func() tea.Msg {
		out, err := exec.Command("reminders", "complete", list, idx).Output()
		if err != nil {
			return inputResultMsg{err: fmt.Errorf("complete task: %w", err)}
		}
		return inputResultMsg{output: strings.TrimSpace(string(out))}
	}
}

// taskNew runs `reminders add <list> "<text>"`.
func taskNew(args []string) tea.Cmd {
	if len(args) < 2 {
		return func() tea.Msg {
			return inputResultMsg{err: fmt.Errorf("usage: task new <list> <text>")}
		}
	}
	list := args[0]
	text := strings.Join(args[1:], " ")
	return func() tea.Msg {
		out, err := exec.Command("reminders", "add", list, text).Output()
		if err != nil {
			return inputResultMsg{err: fmt.Errorf("add task: %w", err)}
		}
		return inputResultMsg{output: strings.TrimSpace(string(out))}
	}
}

// launchTaskEdit fetches tasks, opens fzf, and on select returns taskEditSelectedMsg.
func launchTaskEdit(_ []string) tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("reminders", "show-all", "--format", "json").Output()
		if err != nil {
			return inputResultMsg{err: fmt.Errorf("reminders: %w", err)}
		}
		var items []taskItem
		if jsonErr := json.Unmarshal(out, &items); jsonErr != nil {
			return inputResultMsg{err: fmt.Errorf("parse tasks: %w", jsonErr)}
		}
		var pending []taskItem
		for _, it := range items {
			if !it.IsCompleted {
				pending = append(pending, it)
			}
		}
		if len(pending) == 0 {
			return inputResultMsg{output: "No pending tasks."}
		}
		return taskReadyMsg{items: pending, editMode: true}
	}
}

// execTaskEditor opens a task as YAML in $EDITOR with a yaml-language-server schema modeline.
func execTaskEditor(item taskItem) tea.Cmd {
	schemaPath, schemaErr := ensureTaskSchema()
	content := taskToClaraItem(item)

	// Prepend yaml-language-server modeline if we have a schema.
	if schemaErr == nil && schemaPath != "" {
		content = "# yaml-language-server: $schema=" + schemaPath + "\n" + content
	}

	tmp, err := os.CreateTemp("", "clara-task-*.yaml")
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
		return editDoneMsg{err: err}
	})
}

// taskToClaraItem renders a taskItem as ClaraItem YAML+md for display/editing.
func taskToClaraItem(r taskItem) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("id:     %s\n", r.ExternalID))
	sb.WriteString("type:   task\n")
	sb.WriteString("source: reminders\n")
	sb.WriteString(fmt.Sprintf("source_ref: %s\n", r.ExternalID))
	sb.WriteString(fmt.Sprintf("status: %s\n", func() string {
		if r.IsCompleted {
			return "done"
		}
		return "pending"
	}()))
	if r.DueDate != "" {
		sb.WriteString(fmt.Sprintf("due:    %s\n", r.DueDate))
	}
	if r.Priority > 0 {
		priorities := []string{"", "high", "medium", "low"}
		p := "medium"
		if r.Priority < len(priorities) {
			p = priorities[r.Priority]
		}
		sb.WriteString(fmt.Sprintf("priority: %s\n", p))
	}
	sb.WriteString("action_surface: cloud\n")
	sb.WriteString(fmt.Sprintf("list:   %s\n", r.List))
	sb.WriteString("---\n")
	sb.WriteString(r.Title)
	if r.Notes != "" {
		sb.WriteString("\n\n" + r.Notes)
	}
	return sb.String()
}

// ensureTaskSchema writes the task YAML JSON Schema to ~/.config/clara/schemas/task.schema.json
// and returns its path. The schema enables yaml-language-server to provide completions and validation.
func ensureTaskSchema() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "clara", "schemas")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "task.schema.json")

	schema := `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$id": "https://clara.local/schemas/task.schema.json",
  "title": "ClaraTask",
  "description": "A Clara task item sourced from Apple Reminders or another task system.",
  "type": "object",
  "properties": {
    "id": {
      "type": "string",
      "description": "Unique identifier (e.g. Apple Reminders externalId)"
    },
    "type": {
      "type": "string",
      "enum": ["task", "reminder", "note", "suggestion"],
      "description": "Item type"
    },
    "source": {
      "type": "string",
      "enum": ["reminders", "notes", "taskwarrior", "email", "manual"],
      "description": "System this item was sourced from"
    },
    "source_ref": {
      "type": "string",
      "description": "Source-system reference (e.g. Apple externalId UUID)"
    },
    "status": {
      "type": "string",
      "enum": ["pending", "in_progress", "done", "cancelled"],
      "description": "Current status of the task"
    },
    "priority": {
      "type": "string",
      "enum": ["high", "medium", "low"],
      "description": "Task priority (maps to Apple Reminders priority 1=high, 2=medium, 3=low)"
    },
    "due": {
      "type": "string",
      "format": "date-time",
      "description": "Due date/time (ISO 8601)"
    },
    "action_surface": {
      "type": "string",
      "enum": ["cloud", "local"],
      "description": "Where this item is actionable: cloud (synced via iCloud) or local"
    },
    "list": {
      "type": "string",
      "description": "Apple Reminders list name"
    },
    "tags": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Optional tags"
    }
  },
  "required": ["type", "source", "status"],
  "additionalProperties": false
}
`
	if err := os.WriteFile(path, []byte(schema), 0644); err != nil {
		return "", err
	}
	return path, nil
}



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
			"Server status: %s\nUptime:         %s\nDocuments:      %d ingested\nSuggestions:    %d pending  %d approved  %d rejected",
			s.Status, s.Uptime, s.Documents,
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
	ingested := int64(0)
	if v, ok := resp["files_ingested"].(float64); ok {
		ingested = int64(v)
	}
	return fmt.Sprintf(
		"Agent: running (pid %d)\nUptime:          %s\nNotes directory: %s\nServer:          %s\nFiles ingested:  %d\nActions applied: %d",
		pid, uptime, notesDir, serverAddr, ingested, applied,
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

