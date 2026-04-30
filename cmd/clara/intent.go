package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/brightpuddle/clara/internal/intentlog"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/tui"
	"github.com/charmbracelet/lipgloss"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	intentLogsFollow   bool
	intentLogsVerbose  bool
	intentLogsTail     int
	intentLogsClear    bool
	intentRunVerbose   bool
	intentStartFollow  bool
	intentStartVerbose bool
	intentStartTail    int
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
	Use:   "logs [id] [task]",
	Short: "Show intent execution events",
	Long: `Show structured events from intent log files.

Without -f, all matching events are printed and the command exits.
With -f, historical events are shown then new events are streamed until interrupted.
Use --tail N to limit the initial output to the last N events.
Use --clear to delete log files instead of reading them.`,
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
	intentLogsCmd.Flags().
		IntVar(&intentLogsTail, "tail", 0, "show only the last N events")
	intentLogsCmd.Flags().
		BoolVar(&intentLogsClear, "clear", false, "delete intent log files")
	intentStartCmd.Flags().
		BoolVarP(&intentStartFollow, "follow", "f", false, "follow run output after starting")
	intentStartCmd.Flags().
		BoolVarP(&intentStartVerbose, "verbose", "v", false, "show full tool args/results")
	intentStartCmd.Flags().
		IntVar(&intentStartTail, "tail", 0, "show only the last N events when following")
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

	const colSep = 3
	triggerW := max(termWidth-idW-handlerW-statusW-colSep*3, len("TRIGGER"))
	totalW := idW + handlerW + statusW + triggerW + colSep*3

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

	if intentStartFollow {
		var runID string
		var startedAt time.Time
		if m, ok := resp.Data.(map[string]any); ok {
			runID, _ = m["run_id"].(string)
			if s, ok := m["started_at"].(string); ok {
				startedAt, _ = time.Parse(time.RFC3339Nano, s)
			}
		}
		logPath := filepath.Join(cfg.IntentLogsDir(), intentID+".log")
		filter := intentlog.Filter{RunID: runID, Since: startedAt}
		return followSingleIntentLog(cmd.Context(), logPath, runID, filter, intentStartTail, intentStartVerbose, cfg.DBPath())
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
	taskName := ""
	if len(args) >= 1 {
		intentID = args[0]
	}
	if len(args) >= 2 {
		taskName = args[1]
	}

	logsDir := cfg.IntentLogsDir()
	filter := intentlog.Filter{Entrypoint: taskName}

	// --clear: truncate log files and exit.
	if intentLogsClear {
		if err := intentlog.ClearEvents(logsDir, intentID); err != nil {
			return err
		}
		fmt.Println("Intent events cleared.")
		return nil
	}

	theme := tui.DetectTheme()
	printer := newIntentWatchPrinter(&theme, intentID == "", intentLogsVerbose)

	// Collect historical events.
	var (
		events []intentlog.Event
		err    error
	)
	if intentID != "" {
		logPath := filepath.Join(logsDir, intentID+".log")
		events, err = intentlog.ReadEvents(logPath, filter, intentLogsTail)
	} else {
		events, err = intentlog.MergeEvents(logsDir, filter, intentLogsTail)
	}
	if err != nil {
		return errors.Wrap(err, "read intent events")
	}

	if len(events) == 0 && !intentLogsFollow {
		fmt.Println(theme.Dimmed("No events found."))
		return nil
	}

	for _, event := range events {
		printer.printEvent(event)
	}

	if !intentLogsFollow {
		return nil
	}

	// Follow mode: stream new events until interrupted.
	if intentID != "" {
		// Single-intent follow: use tail -F for efficient inode-following.
		logPath := filepath.Join(logsDir, intentID+".log")
		return followSingleIntentLog(cmd.Context(), logPath, "", filter, 0, intentLogsVerbose, "")
	}

	// Cross-intent follow: poll all *.log files with byte-offset cursors.
	return followAllIntentLogs(cmd.Context(), logsDir, filter, intentLogsVerbose)
}

// followSingleIntentLog follows a single intent log file using tail -F.
// If runID is set, it exits when that run reaches a terminal state (checked via DB).
// tail controls how many initial events to show (0 = already shown by caller; we skip re-printing).
func followSingleIntentLog(ctx context.Context, logPath, runID string, filter intentlog.Filter, tail int, verbose bool, dbPath string) error {
	theme := tui.DetectTheme()
	printer := newIntentWatchPrinter(&theme, false, verbose)

	// Print historical tail if requested (only used from runOneOff/inline follow).
	if tail > 0 {
		events, err := intentlog.ReadEvents(logPath, filter, tail)
		if err != nil {
			return err
		}
		for _, e := range events {
			printer.printEvent(e)
		}
	}

	fmt.Println(theme.Dimmed("Following events... (Ctrl-C to stop)"))

	tailArgs := []string{"-n", "0", "-F", logPath}
	cmd := exec.CommandContext(ctx, "tail", tailArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return errors.Wrapf(err, "start tail %s", logPath)
	}

	// If we need to watch a specific runID for completion, do so in background.
	done := make(chan struct{})
	if runID != "" && dbPath != "" {
		go func() {
			defer close(done)
			logger := buildLogger()
			db, err := store.Open(dbPath, logger)
			if err != nil {
				return
			}
			defer db.Close()
			ticker := time.NewTicker(300 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					run, _, err := db.LoadRun(ctx, runID)
					if err == nil && run.Status != "running" && run.Status != "waiting" {
						return
					}
				}
			}
		}()
	} else {
		close(done)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		for scanner.Scan() {
			line := scanner.Bytes()
			var e intentlog.Event
			if err := json.Unmarshal(line, &e); err != nil {
				continue
			}
			if filter.Matches(e) {
				printer.printEvent(e)
			}
		}
	}()

	// Exit when the run finishes or the context is cancelled.
	select {
	case <-done:
		// Give the scanner a moment to flush any final events.
		time.Sleep(500 * time.Millisecond)
	case <-ctx.Done():
	}

	_ = cmd.Process.Signal(os.Interrupt)
	<-scanDone
	_ = cmd.Wait()
	return nil
}

// followAllIntentLogs polls all *.log files in dir with byte-offset cursors.
// This is used for `clara intent logs -f` (no intentID filter).
func followAllIntentLogs(ctx context.Context, dir string, filter intentlog.Filter, verbose bool) error {
	theme := tui.DetectTheme()
	printer := newIntentWatchPrinter(&theme, true, verbose)

	fmt.Println(theme.Dimmed("Following events... (Ctrl-C to stop)"))

	// offsets tracks the byte offset we've read up to for each file.
	offsets := map[string]int64{}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			paths, _ := filepath.Glob(filepath.Join(dir, "*.log"))
			for _, path := range paths {
				if err := flushFileEvents(path, filter, printer, offsets); err != nil {
					// Non-fatal; file may have been truncated or rotated.
					offsets[path] = 0
				}
			}
		}
	}
}

// flushFileEvents reads new lines from path starting at offsets[path],
// prints matching events, and updates the offset.
func flushFileEvents(path string, filter intentlog.Filter, printer *intentWatchPrinter, offsets map[string]int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	offset := offsets[path]
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var e intentlog.Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if filter.Matches(e) {
			printer.printEvent(e)
		}
	}
	pos, _ := f.Seek(0, io.SeekCurrent)
	offsets[path] = pos
	return scanner.Err()
}

func runOneOff(ctx context.Context, path string, verbose bool) error {
	logger := buildLogger()
	db, err := store.Open(cfg.DBPath(), logger)
	if err != nil {
		return errors.Wrap(err, "open database")
	}
	defer db.Close()

	ilog, err := intentlog.New(cfg.IntentLogsDir())
	if err != nil {
		return errors.Wrap(err, "open intent log dir")
	}
	defer ilog.Close()

	// Load the intent.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	intent := &orchestrator.Intent{
		ID:           strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath)),
		WorkflowType: orchestrator.WorkflowTypeNative,
		Script:       absPath,
	}

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

	coreIntegrations := []string{"fs", "shell", "db"}
	for _, name := range coreIntegrations {
		p := filepath.Join("bin", "integrations", name)
		if _, err := os.Stat(p); err == nil {
			loader := newPluginLoader(reg, nil, cfg, logger)
			if err := loader.loadIntegrationAt(name, p); err != nil {
				logger.Warn().Err(err).Str("name", name).Msg("failed to load core integration for one-off run")
			}
		}
	}

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
	startedAt := time.Now()

	go runIntentInBackground(ctx, intent, runID, "main", nil, reg, db, ilog, logger)

	logPath := ilog.FilePath(intent.ID)
	filter := intentlog.Filter{RunID: runID, Since: startedAt}
	return followSingleIntentLog(ctx, logPath, runID, filter, 0, verbose, cfg.DBPath())
}

// intentWatchPrinter formats intentlog.Event values for terminal display.
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

func (p *intentWatchPrinter) printEvent(event intentlog.Event) {
	ts := event.Time.Format("3:04:05PM")

	if event.Action == "print" {
		p.printRule()
		header := []string{p.theme.Dimmed(ts)}
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
	header := []string{p.theme.Dimmed(ts)}
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
		fmt.Printf("  %s %s (%s)\n", p.theme.Dimmed("action:"), event.Action, event.State)
	}

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
