package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/theme"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

const (
	launchAgentLabel      = "com.brightpuddle.clara.agent"
	launchAgentFileName   = launchAgentLabel + ".plist"
	agentLifecycleTimeout = 10 * time.Second
	agentPollInterval     = 100 * time.Millisecond
	agentRetryDelay       = 500 * time.Millisecond
	defaultLogTailLines   = 100
	watchLogTailLines     = 10
)

var (
	runLaunchctl = func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "launchctl", args...)
		return cmd.CombinedOutput()
	}
	resolveUserHomeDir = os.UserHomeDir
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage the Clara agent lifecycle",
}

var agentStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the Clara agent as a background daemon",
	RunE:         runAgentStart,
	SilenceUsage: true,
}

var agentStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the running Clara agent",
	RunE:         runAgentStop,
	SilenceUsage: true,
}

var agentStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show agent status and active intents",
	RunE:         runAgentStatus,
	SilenceUsage: true,
}

var (
	agentLogsFollow bool
	agentLogsClear  bool
	agentLogsCmd    = &cobra.Command{
		Use:          "logs",
		Short:        "Show recent daemon logs",
		RunE:         runAgentLogs,
		SilenceUsage: true,
	}
)

func init() {
	agentLogsCmd.Flags().BoolVarP(&agentLogsFollow, "follow", "f", false, "follow log output")
	agentLogsCmd.Flags().BoolVar(&agentLogsClear, "clear", false, "clear agent log file")
	agentCmd.AddCommand(agentStartCmd, agentStopCmd, agentStatusCmd, agentLogsCmd)
}

// runDaemonize starts the Clara agent via launchctl. It is shared between
// 'clara agent start' and 'clara serve -d'.
func runDaemonize(ctx context.Context) error {
	if isRunning(cfg.ControlSocketPath()) {
		fmt.Println("Clara agent is already running.")
		return nil
	}

	plistPath, err := launchAgentPlistPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(plistPath); err != nil {
		if os.IsNotExist(err) {
			return errors.Newf(
				"launch agent not installed at %q; run `make install` first",
				plistPath,
			)
		}
		return errors.Wrapf(err, "stat launch agent plist %q", plistPath)
	}

	ctx, cancel := context.WithTimeout(ctx, agentLifecycleTimeout)
	defer cancel()

	loaded, err := launchAgentLoaded(ctx)
	if err != nil {
		return err
	}
	if loaded {
		if err := launchAgentKickstartWithRetry(ctx); err != nil {
			return err
		}
	} else {
		if err := launchAgentBootstrap(ctx, plistPath); err != nil {
			return err
		}
	}

	if err := waitForAgentState(ctx, cfg.ControlSocketPath(), true); err != nil {
		return err
	}
	fmt.Println("Clara agent started.")
	return nil
}

func runAgentStart(cmd *cobra.Command, args []string) error {
	return runDaemonize(cmd.Context())
}

func runAgentStop(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), agentLifecycleTimeout)
	defer cancel()

	running := isRunning(cfg.ControlSocketPath())
	loaded, err := launchAgentLoaded(ctx)
	if err != nil {
		return err
	}
	if !running && !loaded {
		fmt.Println("Clara agent is not running.")
		return nil
	}

	if running {
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodShutdown})
		if err != nil {
			return errors.Wrap(err, "request graceful shutdown")
		}
		if resp.Message != "" {
			fmt.Println(resp.Message)
		}
	}

	if loaded {
		if err := launchAgentBootout(ctx); err != nil {
			return err
		}
	}

	cleanupOrphanedClaraMCPProcesses(ctx)

	if err := waitForAgentState(ctx, cfg.ControlSocketPath(), false); err != nil {
		return err
	}
	fmt.Println("Clara agent stopped.")
	return nil
}

func runAgentStatus(cmd *cobra.Command, args []string) error {
	theme := theme.DetectTheme()

	if !isRunning(cfg.ControlSocketPath()) {
		if wantJSON() {
			prettyPrint(map[string]any{"running": false})
		} else {
			fmt.Println(theme.Dimmed("status:"), "not running")
		}
		return nil
	}

	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodStatus})
	if err != nil {
		return fmt.Errorf("status request failed: %w", err)
	}

	if wantJSON() {
		prettyPrint(resp.Data)
		return nil
	}

	// Human-friendly table output.
	fields, _ := resp.Data.(map[string]any)
	fmt.Printf("  %s %s\n", theme.Dimmed("status:        "), theme.Green("running"))
	if v, ok := fields["servers"]; ok {
		fmt.Printf("  %s %v\n", theme.Dimmed("servers:       "), v)
	}
	if intents, ok := fields["intents"]; ok {
		active := fields["active_intents"]
		fmt.Printf("  %s %v  %s\n",
			theme.Dimmed("intents:       "),
			intents,
			theme.Dimmed(fmt.Sprintf("(%v active)", active)),
		)
	}
	if v, ok := fields["tools"]; ok {
		fmt.Printf("  %s %v\n", theme.Dimmed("tools:         "), v)
	}
	if v, ok := fields["dynamic_mcp"]; ok {
		fmt.Printf("  %s %v\n", theme.Dimmed("dynamic mcp:   "), v)
	}
	return nil
}

func runAgentLogs(cmd *cobra.Command, args []string) error {
	if agentLogsClear {
		if err := os.Truncate(cfg.LogPath(), 0); err != nil && !os.IsNotExist(err) {
			return errors.Wrap(err, "clear agent log")
		}
		fmt.Println("Agent log cleared.")
		return nil
	}
	if agentLogsFollow {
		return followAgentLog(cmd.Context(), cfg.LogPath(), watchLogTailLines)
	}
	return printAgentLogTail(cfg.LogPath(), defaultLogTailLines)
}

func launchAgentPlistPath() (string, error) {
	homeDir, err := resolveUserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "resolve home directory")
	}
	return filepath.Join(homeDir, "Library", "LaunchAgents", launchAgentFileName), nil
}

func launchAgentDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func launchAgentTarget() string {
	return launchAgentDomain() + "/" + launchAgentLabel
}

func launchAgentLoaded(ctx context.Context) (bool, error) {
	_, err := runLaunchctl(ctx, "print", launchAgentTarget())
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, errors.Wrap(err, "query launch agent state")
}

func launchAgentBootstrap(ctx context.Context, plistPath string) error {
	return runLaunchctlCommand(ctx, "bootstrap", launchAgentDomain(), plistPath)
}

func launchAgentKickstart(ctx context.Context) error {
	return runLaunchctlCommand(ctx, "kickstart", "-k", launchAgentTarget())
}

func launchAgentKickstartWithRetry(ctx context.Context) error {
	if err := launchAgentKickstart(ctx); err == nil {
		return nil
	} else if ctx.Err() != nil {
		return err
	}

	timer := time.NewTimer(agentRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "waiting to retry launchctl kickstart")
	case <-timer.C:
	}

	return launchAgentKickstart(ctx)
}

func launchAgentBootout(ctx context.Context) error {
	return runLaunchctlCommand(ctx, "bootout", launchAgentTarget())
}

func runLaunchctlCommand(ctx context.Context, args ...string) error {
	output, err := runLaunchctl(ctx, args...)
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message != "" {
		return errors.Wrapf(err, "launchctl %s: %s", strings.Join(args, " "), message)
	}
	return errors.Wrapf(err, "launchctl %s", strings.Join(args, " "))
}

func cleanupOrphanedClaraMCPProcesses(ctx context.Context) {
	pattern := "(^|/)clara$"
	output, err := exec.CommandContext(ctx, "pgrep", "-f", pattern+".* mcp ").CombinedOutput()
	if err != nil {
		return
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 0 || pid == os.Getpid() {
			continue
		}
		if process, findErr := os.FindProcess(pid); findErr == nil {
			_ = process.Kill()
		}
	}
}

func waitForAgentState(ctx context.Context, socketPath string, wantRunning bool) error {
	check := func() bool {
		running := isRunning(socketPath)
		if wantRunning {
			return running
		}
		return !running
	}
	if check() {
		return nil
	}

	ticker := time.NewTicker(agentPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if wantRunning {
				return errors.New("timed out waiting for Clara agent to start")
			}
			return errors.New("timed out waiting for Clara agent to stop")
		case <-ticker.C:
			if check() {
				return nil
			}
		}
	}
}

func printAgentLogTail(path string, lines int) error {
	logLines, err := tailFileLines(path, lines)
	if err != nil {
		return err
	}
	if len(logLines) == 0 {
		return nil
	}

	useColor := isTerminalFile(os.Stdout)
	for _, line := range logLines {
		fmt.Fprintln(os.Stdout, recolorizeLine(line, useColor))
	}
	return nil
}

func followAgentLog(ctx context.Context, path string, lines int) error {
	tailArgs := []string{"-n", strconv.Itoa(lines), "-F", path}
	cmd := exec.CommandContext(ctx, "tail", tailArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return errors.Wrapf(err, "start tail %s", path)
	}

	useColor := isTerminalFile(os.Stdout)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(os.Stdout, recolorizeLine(line, useColor))
		}
	}()

	return cmd.Wait()
}

func recolorizeLine(line string, useColor bool) string {
	if !strings.HasPrefix(line, "{") {
		return line
	}

	// For performance and to keep it simple, we use a ConsoleWriter to re-format.
	// This might be slightly slow for high-volume logs, but agent logs are typically
	// manageable.
	var out strings.Builder
	writer := zerolog.ConsoleWriter{
		Out:        &out,
		TimeFormat: time.RFC3339,
		NoColor:    !useColor,
	}
	if _, err := writer.Write([]byte(line + "\n")); err != nil {
		return line
	}
	return strings.TrimSpace(out.String())
}

func tailFileLines(path string, maxLines int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.Newf("log file %q does not exist yet", path)
		}
		return nil, errors.Wrapf(err, "open log file %q", path)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	lines := make([]string, 0, maxLines)
	for scanner.Scan() {
		if len(lines) == maxLines {
			copy(lines, lines[1:])
			lines = lines[:maxLines-1]
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.Wrapf(err, "read log file %q", path)
	}
	return lines, nil
}
