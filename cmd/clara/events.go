package main

import (
	"encoding/json"
	"fmt"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var eventCmd = &cobra.Command{
	Use:     "event",
	Aliases: []string{"events"},
	Short:   "Stream and emit CloudEvents on the Clara event bus",
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

var (
	emitSource string
	emitData   string
)

var eventEmitCmd = &cobra.Command{
	Use:   "emit <type>",
	Short: "Emit a CloudEvent to the daemon event bus",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var dataMap map[string]any
		if emitData != "" {
			if err := json.Unmarshal([]byte(emitData), &dataMap); err != nil {
				return errors.Wrap(err, "parse --data JSON")
			}
		} else {
			dataMap = make(map[string]any)
		}

		source := emitSource
		if source == "" {
			source = "cli"
		}

		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodEventEmit,
			Params: map[string]any{
				"type":   args[0],
				"source": source,
				"data":   dataMap,
			},
		})
		if err != nil {
			return err
		}

		if wantJSON() {
			prettyPrint(resp.Data)
			return nil
		}

		fmt.Printf("✓ Emitted event %s (source: %s)\n", args[0], source)
		return nil
	},
}

func init() {
	eventLogsCmd.Flags().IntVarP(&eventTail, "tail", "n", 50, "Number of historical entries to show")
	eventLogsCmd.Flags().BoolVarP(&eventFollow, "follow", "f", false, "Follow real-time entries")
	eventLogsCmd.Flags().StringVar(&eventFilterType, "type", "", "Filter by CloudEvent type")
	eventLogsCmd.Flags().StringVar(&eventFilterSrc, "source", "", "Filter by event source")
	eventCmd.AddCommand(eventLogsCmd)

	eventEmitCmd.Flags().StringVarP(&emitSource, "source", "s", "cli", "event source identifier")
	eventEmitCmd.Flags().StringVarP(&emitData, "data", "d", "", "event data payload JSON")
	eventCmd.AddCommand(eventEmitCmd)

	rootCmd.AddCommand(eventCmd)
}
