package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var (
	intentLogsVerbose  bool
	intentLogsFollow   bool
	intentRunVerbose   bool
	intentResumeInput  string
	intentStartInput   string
	intentStartFollow  bool
	intentStartVerbose bool
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
	Use:   "start <id> [task]",
	Short: "Start an intent task (fires a run for on-demand tasks, activates the loop for schedule/worker/event)",
	Long: `Start an installed intent task.

For on-demand tasks, fires a single run and returns.
For schedule, worker, or event tasks, activates the persistent loop.

Use --input to deliver JSON input to a waiting run instead of starting a new one.
Use --follow/-f to stream run events after starting.`,
	Args:         cobra.RangeArgs(1, 2),
	RunE:         runIntentStart,
	SilenceUsage: true,
}

var intentStopCmd = &cobra.Command{
	Use:          "stop <id> [task]",
	Short:        "Stop a managed schedule, worker, or event task",
	Args:         cobra.RangeArgs(1, 2),
	RunE:         runIntentStop,
	SilenceUsage: true,
}

var intentLogsCmd = &cobra.Command{
	Use:   "logs [id]",
	Short: "Show current intent run states, or follow live with -f",
	Args:  cobra.MaximumNArgs(1),
	Long: `Show a snapshot of current active intent runs and exit.

With -f/--follow the command instead streams live run events continuously
until interrupted (Ctrl-C).`,
	RunE:         runIntentLogs,
	SilenceUsage: true,
}

var intentResumeCmd = &cobra.Command{
	Use:          "resume <run-id>",
	Short:        "Resume a paused Starlark workflow run",
	Args:         cobra.ExactArgs(1),
	RunE:         runIntentResume,
	SilenceUsage: true,
}

var intentEditCmd = &cobra.Command{
	Use:          "edit [intent_name]",
	Short:        "Open an installed intent in $EDITOR",
	Args:         cobra.ExactArgs(1),
	RunE:         runIntentEdit,
	SilenceUsage: true,
}

func init() {
	intentLogsCmd.Flags().
		BoolVarP(&intentLogsVerbose, "verbose", "v", false, "show full tool args/results")
	intentLogsCmd.Flags().
		BoolVarP(&intentLogsFollow, "follow", "f", false, "stream live events until interrupted")
	intentRunCmd.Flags().
		BoolVarP(&intentRunVerbose, "verbose", "v", false, "show full tool args/results")
	intentStartCmd.Flags().
		StringVar(&intentStartInput, "input", "", "JSON value to deliver to the latest waiting run")
	intentStartCmd.Flags().
		BoolVarP(&intentStartFollow, "follow", "f", false, "follow run output after starting")
	intentStartCmd.Flags().
		BoolVarP(&intentStartVerbose, "verbose", "v", false, "show full tool args/results")
	intentResumeCmd.Flags().
		StringVar(&intentResumeInput, "input", "", "JSON value to satisfy the pending wait")
	intentCmd.AddCommand(
		intentListCmd,
		intentRunCmd,
		intentResumeCmd,
		intentStartCmd,
		intentStopCmd,
		intentLogsCmd,
		intentEditCmd,
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

	// Human-friendly table: decode list of task entries.
	items, _ := resp.Data.([]any)
	if len(items) == 0 {
		theme := tui.DetectTheme()
		fmt.Println(theme.Dimmed("No intents registered."))
		return nil
	}

	theme := tui.DetectTheme()

	type task struct {
		Handler string
		Mode    string
		Detail  string
	}
	type intentGroup struct {
		ID          string
		Description string
		Error       string
		Active      bool
		Tasks       []task
	}

	groups := make(map[string]*intentGroup)
	var groupKeys []string

	for _, raw := range items {
		m, _ := raw.(map[string]any)
		id, _ := m["intent_id"].(string)
		g, ok := groups[id]
		if !ok {
			description, _ := m["description"].(string)
			errText, _ := m["error"].(string)
			active, _ := m["active"].(bool)
			g = &intentGroup{
				ID:          id,
				Description: description,
				Error:       errText,
				Active:      active,
			}
			groups[id] = g
			groupKeys = append(groupKeys, id)
		}

		handler, _ := m["handler"].(string)
		if handler == "" {
			continue
		}

		mode, _ := m["mode"].(string)

		// Pick the most informative scheduling detail.
		detail := ""
		switch {
		case m["trigger"] != nil && m["trigger"] != "":
			detail, _ = m["trigger"].(string)
			if args, ok := m["trigger_args"].(map[string]any); ok && len(args) > 0 {
				argParts := []string{}
				keys := make([]string, 0, len(args))
				for k := range args {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					argParts = append(argParts, fmt.Sprintf("%s=%v", k, args[k]))
				}
				detail = fmt.Sprintf("%s(%s)", detail, strings.Join(argParts, ", "))
			}
		case m["schedule"] != nil && m["schedule"] != "":
			detail, _ = m["schedule"].(string)
		case m["interval"] != nil && m["interval"] != "" && m["interval"] != "0s":
			detail, _ = m["interval"].(string)
		}

		g.Tasks = append(g.Tasks, task{
			Handler: handler,
			Mode:    mode,
			Detail:  detail,
		})
	}

	sort.Strings(groupKeys)

	for _, id := range groupKeys {
		g := groups[id]
		activeMarker := theme.Dimmed("·")
		if g.Active {
			activeMarker = theme.Green("●")
		}
		if g.Error != "" {
			activeMarker = theme.Red("✗")
		}

		fmt.Printf("  %s  %s\n", activeMarker, theme.Cyan(g.ID))

		if g.Error != "" {
			fmt.Printf("     %s %s\n", theme.Red("error:"), g.Error)
		} else {
			if g.Description != "" {
				fmt.Printf("     %s\n", g.Description)
			}

			maxHandler, maxMode := 0, 0
			for _, t := range g.Tasks {
				if len(t.Handler) > maxHandler {
					maxHandler = len(t.Handler)
				}
				if len(t.Mode) > maxMode {
					maxMode = len(t.Mode)
				}
			}

			for _, t := range g.Tasks {
				fmt.Printf(
					"       %-*s  %-*s  %s\n",
					maxHandler, t.Handler,
					maxMode, theme.Dimmed(t.Mode),
					theme.Dimmed(t.Detail),
				)
			}
		}
	}
	return nil
}

func runIntentRun(cmd *cobra.Command, args []string) error {
	return runOneOff(cmd.Context(), args[0], intentRunVerbose)
}

func runIntentStart(cmd *cobra.Command, args []string) error {
	intentID := args[0]
	params := map[string]any{"id": intentID}
	if len(args) == 2 {
		params["task"] = args[1]
	}
	if strings.TrimSpace(intentStartInput) != "" {
		var input any
		if err := json.Unmarshal([]byte(intentStartInput), &input); err != nil {
			return errors.Wrap(err, "parse --input JSON")
		}
		params["input"] = input
	}
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodStart,
		Params: params,
	})
	if err != nil {
		return fmt.Errorf("start request failed: %w", err)
	}
	writeResp := resp.Message
	var runID string
	if data, ok := resp.Data.(map[string]any); ok {
		runID, _ = data["run_id"].(string)
	}

	fmt.Println(writeResp)

	if runID != "" {
		return followIntentEvents(cmd.Context(), intentID, runID, intentStartFollow, intentStartVerbose)
	}
	return nil
}

func runIntentStop(cmd *cobra.Command, args []string) error {
	params := map[string]any{"id": args[0]}
	if len(args) == 2 {
		params["task"] = args[1]
	}
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodStop,
		Params: params,
	})
	if err != nil {
		return fmt.Errorf("stop request failed: %w", err)
	}
	fmt.Println(resp.Message)
	return nil
}

func runIntentResume(cmd *cobra.Command, args []string) error {
	logger := buildLogger()
	db, err := store.Open(cfg.DBPath(), logger)
	if err != nil {
		return errors.Wrap(err, "open database")
	}
	defer db.Close()

	runState, _, err := db.LoadRun(cmd.Context(), args[0])
	if err != nil {
		return errors.Wrap(err, "load run")
	}
	if runState.WorkflowType != orchestrator.WorkflowTypeStarlark {
		return errors.New("intent resume currently supports only starlark workflow runs")
	}
	if runState.ScriptSource == "" {
		return errors.New("stored starlark script source is empty")
	}

	if strings.TrimSpace(intentResumeInput) != "" {
		var input any
		if err := json.Unmarshal([]byte(intentResumeInput), &input); err != nil {
			return errors.Wrap(err, "parse --input JSON")
		}
		if err := appendWaitInput(cmd.Context(), db, runState, input); err != nil {
			return err
		}
	}

	reg := registry.New(logger)
	if err := addMCPServers(reg, logger); err != nil {
		return err
	}
	registerPermanentTUITools(reg, db, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	startCtx, startCancel := context.WithCancel(ctx)
	defer startCancel()
	if err := reg.StartServers(startCtx); err != nil {
		return errors.Wrap(err, "start MCP servers")
	}
	defer reg.StopServers()

	printer := newIntentWatchPrinter(tui.DetectTheme(), true, false)
	lastEventID, err := db.LatestRunEventID(ctx, "")
	if err != nil {
		return errors.Wrap(err, "load latest run event id")
	}

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- resumeStoredStarlarkRun(ctx, runState, reg, db, logger)
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-runErrCh:
			if flushErr := flushRunEvents(ctx, db, printer, &lastEventID, runState.RunID); flushErr != nil {
				return flushErr
			}
			if err != nil {
				return err
			}
			return nil
		case <-ticker.C:
			if err := flushRunEvents(ctx, db, printer, &lastEventID, runState.RunID); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func runIntentEdit(cmd *cobra.Command, args []string) error {
	intentID := args[0]
	path, err := findIntentPath(intentID)
	if err != nil {
		return err
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim" // Default to vim
	}

	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func findIntentPath(intentID string) (string, error) {
	tasksDir := cfg.TasksDir()
	// 1. Check if intentID.star exists directly in tasksDir
	path := filepath.Join(tasksDir, intentID+".star")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// 2. Search recursively in tasksDir
	var found string
	stopErr := fmt.Errorf("stop")
	err := filepath.WalkDir(tasksDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(p), ".star") {
			id := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			if id == intentID {
				found = p
				return stopErr
			}
		}
		return nil
	})
	if errors.Is(err, stopErr) {
		return found, nil
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("intent %q not found in %s", intentID, tasksDir)
}

func runIntentLogs(cmd *cobra.Command, args []string) error {
	intentID := ""
	if len(args) == 1 {
		intentID = args[0]
	}

	logger := buildLogger()
	db, err := store.Open(cfg.DBPath(), logger)
	if err != nil {
		return errors.Wrap(err, "open database")
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	printer := newIntentWatchPrinter(tui.DetectTheme(), intentLogsVerbose, intentID == "")
	if err := printer.printCurrentStates(ctx, db, intentID); err != nil {
		return err
	}

	// Without -f, print the snapshot and exit.
	if !intentLogsFollow {
		return nil
	}

	lastEventID, err := db.LatestRunEventID(ctx, intentID)
	if err != nil {
		return errors.Wrap(err, "load latest run event id")
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			events, err := db.RunEventsSince(ctx, lastEventID, intentID)
			if err != nil {
				return errors.Wrap(err, "load run events")
			}
			for _, event := range events {
				printer.printEvent(event)
				lastEventID = event.ID
			}
		}
	}
}

// followIntentEvents opens the DB and streams run events for intentID until
// interrupted. Used by 'intent start -f' and 'intent start'.
func followIntentEvents(parent context.Context, intentID, runID string, follow, verbose bool) error {
	logger := buildLogger()
	db, err := store.Open(cfg.DBPath(), logger)
	if err != nil {
		return errors.Wrap(err, "open database")
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	printer := newIntentWatchPrinter(tui.DetectTheme(), verbose, false)

	var lastEventID int64
	if runID != "" {
		// If we are following a specific run we just started, start from the
		// beginning of that run.
		lastEventID = 0
	} else {
		// No runID: print current active states for the intent (or all) and
		// then follow from now.
		if err := printer.printCurrentStates(ctx, db, intentID); err != nil {
			return err
		}

		var err error
		lastEventID, err = db.LatestRunEventID(ctx, intentID)
		if err != nil {
			return errors.Wrap(err, "load latest run event id")
		}
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			var events []store.RunEvent
			var err error
			if runID != "" {
				events, err = db.RunEventsForRunSince(ctx, lastEventID, runID)
			} else {
				events, err = db.RunEventsSince(ctx, lastEventID, intentID)
			}
			if err != nil {
				return errors.Wrap(err, "load run events")
			}
			for _, event := range events {
				printer.printEvent(event)
				lastEventID = event.ID
			}

			// If we are not following (-f flag omitted), exit once the run
			// finishes.
			if !follow && runID != "" {
				state, _, err := db.LoadRun(ctx, runID)
				if err != nil {
					return errors.Wrap(err, "check run completion")
				}
				if state.Status == "completed" || state.Status == "failed" || state.Status == "cancelled" {
					return nil
				}
			}
		}
	}
}

// ── Printer ───────────────────────────────────────────────────────────────────

type intentWatchPrinter struct {
	theme     tui.Theme
	verbose   bool
	showID    bool
	lastState map[string]string
}

func newIntentWatchPrinter(theme tui.Theme, verbose, showID bool) *intentWatchPrinter {
	return &intentWatchPrinter{
		theme:     theme,
		verbose:   verbose,
		showID:    showID,
		lastState: make(map[string]string),
	}
}

func (p *intentWatchPrinter) printCurrentStates(
	ctx context.Context,
	db *store.Store,
	intentID string,
) error {
	states, err := db.ActiveRunStates(ctx, intentID)
	if err != nil {
		return errors.Wrap(err, "load active intent states")
	}

	p.printRule()
	if len(states) == 0 {
		// Fallback to latest run for this intent if specified.
		if intentID != "" {
			latest, err := db.LatestRunState(ctx, intentID)
			if err != nil {
				return err
			}
			if latest != nil {
				fmt.Printf("%s %s %s\n", p.theme.Dimmed("intent:"), p.paintIntentID(intentID), p.theme.Dimmed("(latest run)"))
				p.lastState[latest.RunID] = latest.State
				p.printStateSnapshot(*latest)
				p.printRule()
				return nil
			}
		}

		if intentID == "" {
			fmt.Println(p.theme.Dimmed("No active intents. Waiting for events..."))
		} else {
			fmt.Printf("%s %s\n", p.theme.Dimmed("intent:"), p.paintIntentID(intentID))
			fmt.Println(p.theme.Dimmed("No active runs."))
		}
		p.printRule()
		return nil
	}

	sort.Slice(states, func(i, j int) bool {
		if states[i].IntentID == states[j].IntentID {
			return states[i].RunID < states[j].RunID
		}
		return states[i].IntentID < states[j].IntentID
	})

	fmt.Println(p.theme.Dimmed("Active runs"))
	for _, state := range states {
		p.lastState[state.RunID] = state.State
		p.printStateSnapshot(state)
	}
	p.printRule()
	return nil
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
	// Print events (from Starlark print()) get a compact dedicated rendering.
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

	if event.Action != "" {
		fmt.Printf("  %s %s\n", p.theme.Dimmed("action:"), p.theme.Cyan(event.Action))
	}
	if event.Action != "" || event.Args != nil {
		if summary := formatArgsCompact(event.Args); summary != "" {
			fmt.Printf("  %s %s\n", p.theme.Dimmed("args:"), p.theme.Dimmed(summary))
		}
	}
	if status := runStatusFromResult(event.Result); status != "" {
		fmt.Printf("  %s %s\n", p.theme.Dimmed("status:"), status)
	}
	if event.Error != "" {
		fmt.Printf("  %s %s\n", p.theme.Yellow("error:"), event.Error)
	}

	if p.verbose {
		if event.Action != "" || event.Args != nil {
			fmt.Printf("  %s\n", p.theme.Dimmed("args"))
			p.printIndentedJSON(event.Args)
		}
		if event.Result != nil {
			fmt.Printf("  %s\n", p.theme.Dimmed("result"))
			p.printIndentedJSON(event.Result)
		}
	}
}

func (p *intentWatchPrinter) printIndentedJSON(value any) {
	if value == nil {
		fmt.Printf("    %s\n", tui.RenderJSON(p.theme, nil))
		return
	}
	rendered := tui.RenderJSON(p.theme, value)
	for _, line := range strings.Split(rendered, "\n") {
		fmt.Printf("    %s\n", line)
	}
}

func (p *intentWatchPrinter) printRule() {
	fmt.Println(p.theme.Dimmed(strings.Repeat("─", 80)))
}

func (p *intentWatchPrinter) paintIntentID(intentID string) string {
	return p.theme.Cyan(intentID)
}

func formatUnixTime(ts int64) string {
	if ts <= 0 {
		return time.Now().Format("15:04:05")
	}
	return time.Unix(ts, 0).Format("15:04:05")
}

func runStatusFromResult(result any) string {
	fields, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	status, ok := fields["status"].(string)
	if !ok {
		return ""
	}
	return status
}

// formatArgsCompact produces a short inline summary of tool call args so that
// repeated calls to the same tool can be distinguished at a glance.
// It renders map keys in sorted order as key=value pairs, truncating long
// values so the line stays readable. Non-map args are rendered as a single
// truncated JSON snippet.
func formatArgsCompact(args any) string {
	if args == nil {
		return ""
	}
	const maxValueLen = 60
	truncate := func(s string) string {
		if len(s) <= maxValueLen {
			return s
		}
		return s[:maxValueLen] + "…"
	}

	m, ok := args.(map[string]any)
	if !ok {
		// Non-map: render as JSON snippet.
		b, err := json.Marshal(args)
		if err != nil {
			return ""
		}
		return truncate(string(b))
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := m[k]
		var val string
		switch tv := v.(type) {
		case string:
			// Inline strings without quotes for readability; escape newlines.
			val = strings.ReplaceAll(tv, "\n", "↵")
			val = strings.ReplaceAll(val, "\r", "")
		default:
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
