package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/tui"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var (
	intentLogsFollow   bool
	intentLogsVerbose  bool
	intentRunVerbose   bool
	intentStartFollow  bool
	intentStartVerbose bool
	intentStartOutput  string
)

var intentCmd = &cobra.Command{
	Use:   "intent",
	Short: "Manage intents",
}

var intentListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List all active intents",
	RunE:         runIntentList,
	SilenceUsage: true,
}

var intentRunCmd = &cobra.Command{
	Use:          "run <intent-file>",
	Short:        "Execute an intent file and watch it",
	Args:         cobra.ExactArgs(1),
	RunE:         runIntentRun,
	SilenceUsage: true,
}

var intentStartCmd = &cobra.Command{
	Use:   "start <id> [task] [key=value...]",
	Short: "Start an intent task",
	Long: `Start an intent by ID.
If no task is provided, defaults to the only available on-demand task.
Arguments can be provided as key=value pairs.`,
	Args:         cobra.ArbitraryArgs,
	RunE:         runIntentStart,
	SilenceUsage: true,
}

var intentStopCmd = &cobra.Command{
	Use:          "stop <id> [task]",
	Short:        "Stop a running intent task",
	Args:         cobra.ArbitraryArgs,
	RunE:         runIntentStop,
	SilenceUsage: true,
}

var intentLogsCmd = &cobra.Command{
	Use:   "logs [id]",
	Short: "Show current state and recent events for an intent",
	Long: `Show a snapshot of active runs and follow live execution events
until interrupted (Ctrl-C).`,
	RunE:         runIntentLogs,
	SilenceUsage: true,
}

func init() {
	intentRunCmd.Flags().
		BoolVarP(&intentRunVerbose, "verbose", "v", false, "show full tool args/results")
	intentLogsCmd.Flags().
		BoolVarP(&intentLogsFollow, "follow", "f", false, "stream live events until interrupted")
	intentLogsCmd.Flags().
		BoolVarP(&intentLogsVerbose, "verbose", "v", false, "show full tool args/results")
	intentStartCmd.Flags().
		BoolVarP(&intentStartFollow, "follow", "f", false, "follow run output after starting")
	intentStartCmd.Flags().
		BoolVarP(&intentStartVerbose, "verbose", "v", false, "show full tool args/results")
	intentStartCmd.Flags().
		StringVarP(&intentStartOutput, "output", "o", "", "output format (json)")
	intentCmd.AddCommand(
		intentListCmd,
		intentRunCmd,
		intentStartCmd,
		intentStopCmd,
		intentLogsCmd,
	)
}

func runIntentList(cmd *cobra.Command, args []string) error {
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodList})
	if err != nil {
		return fmt.Errorf("list request failed: %w", err)
	}

	if wantJSON() {
		prettyPrint(resp.Data)
		return nil
	}

	data, ok := resp.Data.([]any)
	if !ok {
		return fmt.Errorf("unexpected list response: %T", resp.Data)
	}

	type taskEntry struct {
		IntentID    string         `json:"intent_id"`
		Path        string         `json:"path,omitempty"`
		Description string         `json:"description,omitempty"`
		Handler     string         `json:"handler"`
		Mode        string         `json:"mode"`
		Schedule    string         `json:"schedule,omitempty"`
		Interval    string         `json:"interval,omitempty"`
		Trigger     string         `json:"trigger,omitempty"`
		TriggerArgs map[string]any `json:"trigger_args,omitempty"`
		Active      bool           `json:"active"`
		Error       string         `json:"error,omitempty"`
	}

	var tasks []taskEntry
	for _, item := range data {
		var t taskEntry
		b, _ := json.Marshal(item)
		_ = json.Unmarshal(b, &t)
		tasks = append(tasks, t)
	}

	theme := tui.DetectTheme()

	// Determine terminal width, defaulting to 80 if unavailable.
	termWidth := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		termWidth = w
	}

	// Compute column widths from content.
	idW, handlerW, statusW := len("ID"), len("HANDLER"), len("STATUS")
	for _, t := range tasks {
		if n := len(t.IntentID); n > idW {
			idW = n
		}
		if n := len(t.Handler); n > handlerW {
			handlerW = n
		}
	}
	// STATUS values are fixed: "idle", "active", "error" — statusW stays at header width.

	// 3 separating spaces between 4 columns.
	const colSep = 3
	// TRIGGER column gets the remainder of the terminal width.
	triggerW := max(termWidth-idW-handlerW-statusW-colSep*3, len("TRIGGER"))
	totalW := idW + handlerW + statusW + triggerW + colSep*3

	// Define styles for columns
	idStyle := lipgloss.NewStyle().Width(idW)
	handlerStyle := lipgloss.NewStyle().Width(handlerW).Foreground(theme.Secondary)
	statusStyle := lipgloss.NewStyle().Width(statusW)
	triggerStyle := lipgloss.NewStyle().Width(triggerW)

	headerStyle := lipgloss.NewStyle().Foreground(theme.MagentaColor).Bold(true)

	fmt.Printf("%s   %s   %s   %s\n",
		headerStyle.Width(idW).Render("ID"),
		headerStyle.Width(handlerW).Render("HANDLER"),
		headerStyle.Width(statusW).Render("STATUS"),
		headerStyle.Width(triggerW).Render("TRIGGER"))
	fmt.Println(strings.Repeat("─", totalW))

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].IntentID != tasks[j].IntentID {
			return tasks[i].IntentID < tasks[j].IntentID
		}
		return tasks[i].Handler < tasks[j].Handler
	})

	for _, t := range tasks {
		status := "idle"
		sStyle := statusStyle.Foreground(theme.Dim)
		if t.Active {
			status = "active"
			sStyle = statusStyle.Foreground(theme.Success)
		}
		if t.Error != "" {
			status = "error"
			sStyle = statusStyle.Foreground(theme.Error)
		}

		trigger := "on_demand"
		switch t.Mode {
		case "schedule":
			trigger = t.Schedule
		case "worker":
			if t.Interval != "" {
				trigger = "@every " + t.Interval
			} else {
				trigger = "@every 1s"
			}
		case "event":
			trigger = "event:" + t.Trigger
		}

		fmt.Printf("%s   %s   %s   %s\n",
			idStyle.Render(t.IntentID),
			handlerStyle.Render(t.Handler),
			sStyle.Render(status),
			triggerStyle.Render(trigger))
	}

	return nil
}

func runIntentRun(cmd *cobra.Command, args []string) error {
	return runOneOff(cmd.Context(), args[0], intentRunVerbose)
}

func runIntentStart(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("missing intent ID")
	}
	intentID := args[0]
	params := map[string]any{"id": intentID}
	var trailing []string
	if len(args) > 1 {
		if !strings.Contains(args[1], "=") {
			params["task"] = args[1]
			trailing = args[2:]
		} else {
			trailing = args[1:]
		}
	}

	var intentArgs map[string]any
	if len(trailing) > 0 {
		parsed, err := parseToolCallArgs(trailing)
		if err != nil {
			return err
		}
		intentArgs = parsed
	}

	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodStart,
		Params: params,
		Args:   intentArgs,
	})
	if err != nil {
		return err
	}

	if wantJSON() || intentStartOutput == "json" {
		prettyPrint(resp.Data)
		return nil
	}

	fmt.Println(resp.Message)

	if intentStartFollow || intentStartVerbose {
		var runID string
		var lastEventID int64
		if m, ok := resp.Data.(map[string]any); ok {
			runID, _ = m["run_id"].(string)
			if v, ok := m["last_event_id"].(float64); ok {
				lastEventID = int64(v)
			}
		}
		return followIntentEvents(cmd.Context(), intentID, runID, lastEventID, intentStartVerbose)
	}

	return nil
}

func runIntentStop(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("missing intent ID")
	}
	intentID := args[0]
	params := map[string]any{"id": intentID}
	if len(args) > 1 {
		params["task"] = args[1]
	}
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodStop,
		Params: params,
	})
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}

func runIntentLogs(cmd *cobra.Command, args []string) error {
	intentID := ""
	if len(args) == 1 {
		intentID = args[0]
	}

	if intentLogsFollow {
		return followIntentEvents(cmd.Context(), intentID, "", 0, intentLogsVerbose)
	}

	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodStatus,
		Params: map[string]any{"id": intentID},
	})
	if err != nil {
		return err
	}

	if wantJSON() {
		prettyPrint(resp.Data)
		return nil
	}

	states, ok := resp.Data.([]any)
	if !ok {
		return fmt.Errorf("unexpected status response: %T", resp.Data)
	}

	theme := tui.DetectTheme()
	printer := newIntentWatchPrinter(&theme, true, intentLogsVerbose)

	if len(states) == 0 {
		fmt.Println(theme.Dimmed("No active runs."))
		return nil
	}

	fmt.Println(theme.Dimmed("Active runs"))
	for _, s := range states {
		var state store.RunState
		b, _ := json.Marshal(s)
		_ = json.Unmarshal(b, &state)
		printer.printStateSnapshot(state)
	}
	printer.printRule()

	return nil
}

func runOneOff(ctx context.Context, path string, verbose bool) error {
	logger := buildLogger()
	db, err := store.Open(cfg.DBPath(), logger)
	if err != nil {
		return errors.Wrap(err, "open database")
	}
	defer db.Close()

	// 1. Load the intent
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	
	// For runOneOff, we assume it's a native plugin if it's executable
	// and doesn't look like YAML/JSON.
	intent := &orchestrator.Intent{
		ID:           strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath)),
		WorkflowType: orchestrator.WorkflowTypeNative,
		Script:       absPath,
	}
	
	// If it's a YAML/JSON file, try parsing it as a state machine
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".json") {
		data, err := os.ReadFile(absPath)
		if err != nil {
			return err
		}
		intent, err = orchestrator.ParseIntent(data)
		if err != nil {
			return err
		}
	}

	reg := registry.New(logger)
	registerPermanentTUITools(reg, db, logger)

	// Load core integrations if they exist in bin/integrations
	coreIntegrations := []string{"fs", "shell", "db"}
	for _, name := range coreIntegrations {
		path := filepath.Join("bin", "integrations", name)
		if _, err := os.Stat(path); err == nil {
			loader := newPluginLoader(reg, nil, cfg, logger)
			if err := loader.loadIntegrationAt(name, path); err != nil {
				logger.Warn().Err(err).Str("name", name).Msg("failed to load core integration for one-off run")
			}
		}
	}

	// Load macos (ClaraBridge) if it exists
	macosPaths := []string{
		"/usr/local/libexec/ClaraBridge.app/Contents/MacOS/ClaraBridge",
		"./build/ClaraBridge.app/Contents/MacOS/ClaraBridge",
	}
	for _, p := range macosPaths {
		if _, err := os.Stat(p); err == nil {
			loader := newPluginLoader(reg, nil, cfg, logger)
			if err := loader.loadIntegrationAt("macos", p); err != nil {
				logger.Warn().Err(err).Msg("failed to load macos integration for one-off run")
			}
			break
		}
	}

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	runID := fmt.Sprintf("%s-oneoff-%d", intent.ID, time.Now().UnixNano())
	theme := tui.DetectTheme()
	printer := newIntentWatchPrinter(&theme, true, verbose)

	lastEventID, err := db.LatestRunEventID(ctx, "")
	if err != nil {
		return errors.Wrap(err, "load latest run event id")
	}

	go runIntentInBackground(ctx, intent, runID, "main", nil, reg, db, logger)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := flushRunEvents(ctx, db, printer, &lastEventID, runID); err != nil {
				return err
			}
			
			// Check if run finished
			run, _, err := db.LoadRun(ctx, runID)
			if err == nil && run.Status != "running" && run.Status != "waiting" {
				return nil
			}
		}
	}
}

// followIntentEvents streams run events to stdout until the run finishes or ctx
// is cancelled. runID and lastEventID should come from the MethodStart response
// so the baseline is captured before the goroutine begins (avoiding the race
// where a fast run completes before the client opens the DB). When runID is
// empty (e.g. plain "intent logs -f") we fall back to querying the latest ID
// ourselves and follow without an exit condition.
func followIntentEvents(ctx context.Context, intentID, runID string, lastEventID int64, verbose bool) error {
	logger := buildLogger()
	db, err := store.Open(cfg.DBPath(), logger)
	if err != nil {
		return errors.Wrap(err, "open database")
	}
	defer db.Close()

	theme := tui.DetectTheme()
	printer := newIntentWatchPrinter(&theme, intentID == "", verbose)

	// If no baseline was supplied (legacy / logs -f path), query it now.
	if runID == "" && lastEventID == 0 {
		lastEventID, err = db.LatestRunEventID(ctx, intentID)
		if err != nil {
			return errors.Wrap(err, "load latest run event id")
		}
	}

	fmt.Println(theme.Dimmed("Following events... (Ctrl-C to stop)"))

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := flushRunEvents(ctx, db, printer, &lastEventID, runID); err != nil {
				return err
			}
			// Exit once a specific run has reached a terminal state.
			if runID != "" {
				run, _, err := db.LoadRun(ctx, runID)
				if err == nil && run.Status != "running" && run.Status != "waiting" {
					return nil
				}
			}
		}
	}
}

func flushRunEvents(
	ctx context.Context,
	db *store.Store,
	printer *intentWatchPrinter,
	lastID *int64,
	runID string,
) error {
	events, err := db.RunEventsSince(ctx, *lastID, "")
	if err != nil {
		return errors.Wrap(err, "load run events")
	}
	for _, event := range events {
		if runID == "" || event.RunID == runID {
			printer.printEvent(event)
		}
		*lastID = event.ID
	}
	return nil
}

type intentWatchPrinter struct {
	theme     *tui.Theme
	showID    bool
	verbose   bool
	lastState map[string]string // keyed by RunID
}

func newIntentWatchPrinter(theme *tui.Theme, showID, verbose bool) *intentWatchPrinter {
	return &intentWatchPrinter{
		theme:     theme,
		showID:    showID,
		verbose:   verbose,
		lastState: make(map[string]string),
	}
}

func (p *intentWatchPrinter) printRule() {
	fmt.Println(p.theme.Dimmed(strings.Repeat("─", 80)))
}

func (p *intentWatchPrinter) paintIntentID(id string) string {
	return p.theme.Cyan(id)
}

func (p *intentWatchPrinter) printStateSnapshot(state store.RunState) {
	header := []string{p.theme.Dimmed(formatUnixTime(state.UpdatedAt))}
	if p.showID {
		header = append(header, p.paintIntentID(state.IntentID))
	}
	header = append(header, p.theme.Magenta(state.State))
	fmt.Println(strings.Join(header, "  "))
	fmt.Printf("  %s %s\n", p.theme.Dimmed("run:"), state.RunID)
	fmt.Printf("  %s %s\n", p.theme.Dimmed("status:"), state.Status)
}

func (p *intentWatchPrinter) printEvent(event store.RunEvent) {
	if event.Action == "print" {
		p.printRule()
		header := []string{p.theme.Dimmed(formatUnixTime(event.CreatedAt))}
		if p.showID {
			header = append(header, p.paintIntentID(event.IntentID))
		}
		header = append(header, p.theme.Magenta(event.State))
		fmt.Println(strings.Join(header, "  "))
		fmt.Printf("  %s %s\n", p.theme.Dimmed("run:"), event.RunID)
		if msg, ok := event.Result.(string); ok {
			fmt.Printf("  %s %s\n", p.theme.Dimmed("print:"), msg)
		}
		return
	}

	p.printRule()
	header := []string{p.theme.Dimmed(formatUnixTime(event.CreatedAt))}
	if p.showID {
		header = append(header, p.paintIntentID(event.IntentID))
	}

	if prev := p.lastState[event.RunID]; prev != "" && prev != event.State {
		header = append(header, p.theme.Magenta(prev+" -> "+event.State))
	} else {
		header = append(header, p.theme.Magenta(event.State))
	}
	p.lastState[event.RunID] = event.State

	fmt.Println(strings.Join(header, "  "))
	fmt.Printf("  %s %s\n", p.theme.Dimmed("run:"), event.RunID)
	fmt.Printf("  %s %s (%s)\n", p.theme.Dimmed("action:"), event.Action, event.State)

	if p.verbose {
		if args, ok := event.Args.(map[string]any); ok && len(args) > 0 {
			fmt.Printf("  %s %s\n", p.theme.Dimmed("args:"), formatJSONArgs(args))
		}
	}

	if event.Error != "" {
		fmt.Printf("  %s %s\n", p.theme.Red("error:"), event.Error)
	} else if p.verbose && event.Result != nil {
		fmt.Printf("  %s %v\n", p.theme.Green("result:"), event.Result)
	}
}

func formatUnixTime(t int64) string {
	return time.Unix(0, t).Format("3:04:05PM")
}

func formatJSONArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	var parts []string
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	truncate := func(s string) string {
		if len(s) > 100 {
			return s[:97] + "..."
		}
		return s
	}

	for _, k := range keys {
		v := args[k]
		val := ""
		if s, ok := v.(string); ok {
			val = s
		} else {
			b, err := json.Marshal(v)
			if err != nil {
				val = fmt.Sprintf("%v", v)
			} else {
				val = string(b)
			}
		}
		parts = append(parts, k+"="+truncate(val))
	}
	return strings.Join(parts, " ")
}
