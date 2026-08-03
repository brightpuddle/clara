package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/loghub"
	"github.com/brightpuddle/clara/internal/ringbuf"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Daemon-side: streaming handler wired into the IPC server
// ---------------------------------------------------------------------------

// daemonHandler wraps the regular HandlerFunc and adds stream support via the
// loghub ring buffers.
type daemonHandler struct {
	base ipc.HandlerFunc
	hub  *loghub.Hub
}

func (h *daemonHandler) Handle(ctx context.Context, req *ipc.Request, w ipc.ResponseWriter) {
	h.base.Handle(ctx, req, w)
}

func (h *daemonHandler) HandleStream(ctx context.Context, req *ipc.StreamRequest, w ipc.RawWriter) {
	var buf *ringbuf.RingBuffer
	switch req.Method {
	case ipc.MethodEventLogs:
		buf = h.hub.Event
	case ipc.MethodEvaluatorLogs:
		buf = h.hub.Evaluator
	case ipc.MethodActuatorLogs:
		if req.ActuatorID != "" {
			buf = h.hub.BufFor(req.ActuatorID)
		} else {
			buf = h.hub.Actuator
		}
	default:
		return
	}

	writeEntry := func(entry []byte) bool {
		if req.Method == ipc.MethodEventLogs {
			if req.FilterType != "" || req.FilterSource != "" {
				var m map[string]string
				if err := json.Unmarshal(entry, &m); err == nil {
					if req.FilterType != "" && m["type"] != req.FilterType {
						return true
					}
					if req.FilterSource != "" && m["source"] != req.FilterSource {
						return true
					}
				}
			}
		}
		return w.WriteRaw(entry) == nil
	}

	if !req.Follow {
		for _, entry := range buf.Snapshot(req.Tail) {
			if !writeEntry(entry) {
				return
			}
		}
		return
	}

	sub := buf.Subscribe(ctx, req.Tail)
	for entry := range sub {
		if !writeEntry(entry) {
			return
		}
	}
}

// ---------------------------------------------------------------------------
// CLI-side: stream reader helper
// ---------------------------------------------------------------------------

func streamLogs(socketPath, method string, tail int, follow bool, extra map[string]string) error {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	req := ipc.StreamRequest{
		Method: method,
		Tail:   tail,
		Follow: follow,
	}
	if v, ok := extra["filter_type"]; ok {
		req.FilterType = v
	}
	if v, ok := extra["filter_source"]; ok {
		req.FilterSource = v
	}
	if v, ok := extra["actuator_id"]; ok {
		req.ActuatorID = v
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("send request: %w", err)
	}

	dec := json.NewDecoder(conn)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil // EOF or closed
		}
		fmt.Println(string(raw))
	}
}

// ---------------------------------------------------------------------------
// `clara event` command group
// ---------------------------------------------------------------------------

var eventCmd = &cobra.Command{
	Use:   "event",
	Short: "Observe the ingress event stream",
}

var (
	eventTail       int
	eventFollow     bool
	eventFilterType string
	eventFilterSrc  string
)

var eventLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View and follow CloudEvents on the event bus",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		extra := map[string]string{}
		if eventFilterType != "" {
			extra["filter_type"] = eventFilterType
		}
		if eventFilterSrc != "" {
			extra["filter_source"] = eventFilterSrc
		}
		return streamLogs(cfg.ControlSocketPath(), ipc.MethodEventLogs, eventTail, eventFollow, extra)
	},
}

// ---------------------------------------------------------------------------
// `clara evaluator` command group
// ---------------------------------------------------------------------------

var evaluatorCmd = &cobra.Command{
	Use:   "evaluator",
	Short: "Observe evaluator decisions and memory",
}

var (
	evaluatorTail   int
	evaluatorFollow bool
)

var evaluatorLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View and follow evaluator decision logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		return streamLogs(cfg.ControlSocketPath(), ipc.MethodEvaluatorLogs, evaluatorTail, evaluatorFollow, nil)
	},
}

// ---------------------------------------------------------------------------
// `clara actuator` command group
// ---------------------------------------------------------------------------

var actuatorCmd = &cobra.Command{
	Use:   "actuator",
	Short: "Manage and observe actuator execution",
}

var actuatorListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all loaded actuators",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodActuatorList})
		if err != nil {
			return err
		}
		if resp.Error != "" {
			return fmt.Errorf("%s", resp.Error)
		}
		items, _ := resp.Data.([]any)
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "ID\tDESCRIPTION\tSTATUS")
		for _, item := range items {
			m, _ := item.(map[string]any)
			fmt.Fprintf(tw, "%s\t%s\t%s\n", m["id"], m["description"], m["status"])
		}
		tw.Flush()
		return nil
	},
}

var (
	actuatorLogsTail   int
	actuatorLogsFollow bool
)

var actuatorLogsCmd = &cobra.Command{
	Use:   "logs <id>",
	Short: "View and follow subprocess logs for an actuator",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		return streamLogs(
			cfg.ControlSocketPath(),
			ipc.MethodActuatorLogs,
			actuatorLogsTail,
			actuatorLogsFollow,
			map[string]string{"actuator_id": args[0]},
		)
	},
}

var actuatorRunPayload string
var actuatorRunFollow bool

var actuatorRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Manually dispatch an actuator with an optional JSON payload",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		params := map[string]any{"id": args[0]}
		if actuatorRunPayload != "" {
			var payload any
			if err := json.Unmarshal([]byte(actuatorRunPayload), &payload); err != nil {
				return fmt.Errorf("invalid --payload JSON: %w", err)
			}
			params["payload"] = payload
		}
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodActuatorRun,
			Params: params,
		})
		if err != nil {
			return err
		}
		if resp.Error != "" {
			return fmt.Errorf("%s", resp.Error)
		}
		fmt.Println(resp.Message)

		if actuatorRunFollow {
			return streamLogs(
				cfg.ControlSocketPath(),
				ipc.MethodActuatorLogs,
				0,
				true,
				map[string]string{"actuator_id": args[0]},
			)
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// `clara automations` command group
// ---------------------------------------------------------------------------

var automationsCmd = &cobra.Command{
	Use:   "automations",
	Short: "Inspect active automations, triggers, and actuator mappings",
}

var automationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active workflow automations and routing policies",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := loadConfig(); err != nil {
			return err
		}
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{Method: ipc.MethodAutomationsList})
		if err != nil {
			return err
		}
		if resp.Error != "" {
			return fmt.Errorf("%s", resp.Error)
		}
		items, _ := resp.Data.([]any)
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "ACTUATOR\tROUTING\tTRIGGERS\tTTL / EXPIRES\tDESCRIPTION")
		for _, item := range items {
			m, _ := item.(map[string]any)
			triggers := ""
			if tr, ok := m["triggers"].([]any); ok && len(tr) > 0 {
				strList := make([]string, len(tr))
				for i, v := range tr {
					strList[i] = fmt.Sprint(v)
				}
				triggers = strings.Join(strList, ", ")
			} else {
				triggers = "*"
			}

			ttlInfo := "-"
			if m["routing"] == "fast-path" {
				ttlInfo = fmt.Sprintf("%v (%v left)", m["ttl"], m["expires_in"])
			}

			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				m["actuator_id"],
				m["routing"],
				triggers,
				ttlInfo,
				m["description"],
			)
		}
		tw.Flush()
		return nil
	},
}

// ---------------------------------------------------------------------------
// init: register all commands with the root cobra command
// ---------------------------------------------------------------------------

func init() {
	// event logs flags
	eventLogsCmd.Flags().IntVarP(&eventTail, "tail", "n", 50, "Number of historical entries to show")
	eventLogsCmd.Flags().BoolVarP(&eventFollow, "follow", "f", false, "Follow real-time entries")
	eventLogsCmd.Flags().StringVar(&eventFilterType, "type", "", "Filter by CloudEvent type")
	eventLogsCmd.Flags().StringVar(&eventFilterSrc, "source", "", "Filter by event source")
	eventCmd.AddCommand(eventLogsCmd)

	// evaluator logs flags
	evaluatorLogsCmd.Flags().IntVarP(&evaluatorTail, "tail", "n", 50, "Number of historical entries to show")
	evaluatorLogsCmd.Flags().BoolVarP(&evaluatorFollow, "follow", "f", false, "Follow real-time entries")
	evaluatorCmd.AddCommand(evaluatorLogsCmd)

	// actuator flags
	actuatorLogsCmd.Flags().IntVarP(&actuatorLogsTail, "tail", "n", 50, "Number of historical entries to show")
	actuatorLogsCmd.Flags().BoolVarP(&actuatorLogsFollow, "follow", "f", false, "Follow real-time entries")
	actuatorRunCmd.Flags().StringVar(&actuatorRunPayload, "payload", "", "JSON payload to pass to the actuator")
	actuatorRunCmd.Flags().BoolVarP(&actuatorRunFollow, "follow", "f", false, "Stream actuator logs after dispatching")
	actuatorCmd.AddCommand(actuatorListCmd, actuatorLogsCmd, actuatorRunCmd)

	// automations commands
	automationsCmd.AddCommand(automationsListCmd)

	rootCmd.AddCommand(eventCmd, evaluatorCmd, actuatorCmd, automationsCmd)
}
