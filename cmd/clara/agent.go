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
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

const (
	launchAgentLabel      = "com.brightpuddle.clara.agent"
	launchAgentFileName   = launchAgentLabel + ".plist"
	agentLifecycleTimeout = 10 * time.Second
	agentPollInterval     = 100 * time.Millisecond
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
	Short:        "Start the Clara agent",
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
	agentLogsWatch bool
	agentLogsCmd   = &cobra.Command{
		Use:          "logs",
		Short:        "Show recent daemon logs",
		RunE:         runAgentLogs,
		SilenceUsage: true,
	}
)

func init() {
	agentLogsCmd.Flags().BoolVarP(&agentLogsWatch, "watch", "w", false, "follow log output")
	agentCmd.AddCommand(agentStartCmd, agentStopCmd, agentStatusCmd, agentLogsCmd)
}

func runAgentStart(cmd *cobra.Command, args []string) error {
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

	ctx, cancel := context.WithTimeout(cmd.Context(), agentLifecycleTimeout)
	defer cancel()

	loaded, err := launchAgentLoaded(ctx)
	if err != nil {
		return err
	}
	if loaded {
		if err := launchAgentKickstart(ctx); err != nil {
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

	if err := waitForAgentState(ctx, cfg.ControlSocketPath(), false); err != nil {
		return err
	}
	fmt.Println("Clara agent stopped.")
	return nil
}

func runAgentStatus(cmd *cobra.Command, args []string) error {
	if !isRunning(cfg.ControlSocketPath()) {
		fmt.Println("Clara agent is not running.")
		return nil
	}
	resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodStatus})
	if err != nil {
		return fmt.Errorf("status request failed: %w", err)
	}
	prettyPrint(resp.Data)
	return nil
}

func runAgentLogs(cmd *cobra.Command, args []string) error {
	if agentLogsWatch {
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
	fmt.Fprint(os.Stdout, strings.Join(logLines, "\n"))
	fmt.Fprintln(os.Stdout)
	return nil
}

func followAgentLog(ctx context.Context, path string, lines int) error {
	tailArgs := []string{"-n", strconv.Itoa(lines), "-F", path}
	cmd := exec.CommandContext(ctx, "tail", tailArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return errors.Wrapf(err, "tail %s", path)
	}
	return nil
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
