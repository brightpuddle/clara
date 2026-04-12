package main

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/tui"
	"github.com/spf13/cobra"
)

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Stream real-time events from the Clara daemon",
	Long: `Stream real-time events from the Clara daemon.

This command connects to the daemon and prints every event published to the
internal event bus, such as theme changes, reminder updates, and filesystem
notifications.`,
	RunE: runEvents,
}

func init() {
	rootCmd.AddCommand(eventsCmd)
}

func runEvents(cmd *cobra.Command, args []string) error {
	socketPath := cfg.ControlSocketPath()
	if !isRunning(socketPath) {
		return fmt.Errorf("clara agent is not running")
	}

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return fmt.Errorf("dial control socket: %w", err)
	}
	defer conn.Close()

	req := ipc.Request{Method: ipc.MethodEvents}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("send events request: %w", err)
	}

	theme := tui.DetectTheme()
	fmt.Printf("%s\n", theme.Dimmed("Streaming events... (Ctrl+C to stop)"))

	dec := json.NewDecoder(conn)
	for {
		var resp ipc.Response
		if err := dec.Decode(&resp); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}

		if resp.Error != "" {
			return fmt.Errorf("agent error: %s", resp.Error)
		}

		if resp.Data != nil {
			prettyPrint(resp.Data)
		}
	}
}
