package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/brightpuddle/clara/internal/ipc"
)

const agentTailPollInterval = 500 * time.Millisecond

var (
	agentNewDir    string
	agentNewModel  string
	agentNewKind   string
	agentSendFrom  string
	agentTailAfter int64
	agentTailFlw   bool
	agentListAll   bool
	agentHost      string
)

var agentListCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List managed agent sessions",
	Args:         cobra.NoArgs,
	RunE:         runAgentSessionList,
	SilenceUsage: true,
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage dev agent sessions (agy, opencode)",
	Long: `Manage dev agent sessions (agy, opencode) driven by the Clara daemon.

To control the daemon itself, see 'clara start', 'clara stop' and 'clara status'.`,
}

var agentNewCmd = &cobra.Command{
	Use:   "new <agy|opencode> [prompt]",
	Short: "Start a managed agent session",
	Long: `Start a managed agent session through the running daemon.

Example:
  clara agent new opencode --dir ~/src/clara "summarise the README"`,
	Args:         cobra.RangeArgs(1, 2),
	RunE:         runAgentSessionNew,
	SilenceUsage: true,
}

var agentSendCmd = &cobra.Command{
	Use:          "send <session> <text>",
	Short:        "Queue a prompt for a session",
	Args:         cobra.MinimumNArgs(2),
	RunE:         runAgentSessionSend,
	SilenceUsage: true,
}

var agentTailCmd = &cobra.Command{
	Use:          "tail [session]",
	Short:        "Print logged agent events (all sessions if none given)",
	Args:         cobra.RangeArgs(0, 1),
	RunE:         runAgentSessionTail,
	SilenceUsage: true,
}

var agentCancelCmd = &cobra.Command{
	Use:   "cancel <session> [prompt-id]",
	Short: "Cancel a queued prompt, or interrupt the running turn if no prompt is given",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runAgentSessionCancel,

	SilenceUsage: true,
}

func init() {
	agentListCmd.Flags().BoolVarP(&agentListAll, "all", "a", false, "include closed sessions")
	agentNewCmd.Flags().StringVar(&agentNewDir, "dir", "", "working directory (default: current)")
	agentNewCmd.Flags().StringVar(&agentNewModel, "model", "", "model override")
	agentNewCmd.Flags().StringVar(&agentNewKind, "kind", "", "longlived (default) or task")
	agentSendCmd.Flags().StringVar(&agentSendFrom, "origin", "cli", "origin label for the prompt")
	agentTailCmd.Flags().Int64Var(&agentTailAfter, "after", 0, "start after this host_seq")
	agentTailCmd.Flags().BoolVarP(&agentTailFlw, "follow", "f", false, "keep polling for new events")
	agentCmd.PersistentFlags().StringVar(&agentHost, "host", "",
		"agent host to act on (default: this machine; see agents.remote_hosts)")
	agentCmd.AddCommand(agentListCmd, agentNewCmd, agentSendCmd, agentTailCmd, agentCancelCmd)
}

// agentToolPrefix maps a host name to the tool namespace prefix of its agent
// core: empty for this machine, the configured tool_prefix for a remote.
func agentToolPrefix(host string) (string, error) {
	if host == "" || host == cfg.Agents.HostName() {
		return "", nil
	}
	for _, r := range cfg.Agents.RemoteHosts {
		if r.Name == host {
			return r.ToolPrefix, nil
		}
	}
	return "", errors.Newf("unknown agent host %q (not in agents.remote_hosts)", host)
}

// callAgentTool invokes an agent.* tool on the running daemon.
func callAgentTool(name string, args map[string]any) (map[string]any, error) {
	prefix, err := agentToolPrefix(agentHost)
	if err != nil {
		return nil, err
	}
	name = prefix + name
	resp, err := sendRawRequest(cfg.ControlSocketPath(), ipc.Request{
		Method: ipc.MethodToolCall,
		Params: map[string]any{"name": name},
		Args:   args,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "%s request failed (is the daemon running?)", name)
	}
	if resp.Error != "" {
		return nil, errors.Newf("%s: %s", name, resp.Error)
	}
	m, _ := resp.Data.(map[string]any)
	return m, nil
}

func runAgentSessionList(_ *cobra.Command, _ []string) error {
	out, err := callAgentTool("agent.session.list", nil)
	if err != nil {
		return err
	}
	sessions, _ := out["sessions"].([]any)
	shown := 0
	for _, v := range sessions {
		s, _ := v.(map[string]any)
		if !agentListAll && s["state"] == "closed" {
			continue
		}
		shown++
		fmt.Printf("%-28s %-9s %-9s q=%v %s  %s\n",
			s["id"], s["agent"], s["state"], s["queue_len"], s["dir"], s["title"])
	}
	if shown == 0 {
		fmt.Println("no sessions")
	}
	return nil
}

func runAgentSessionNew(_ *cobra.Command, args []string) error {
	dir := agentNewDir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	req := map[string]any{"agent": args[0], "dir": dir}
	if agentNewModel != "" {
		req["model"] = agentNewModel
	}
	if agentNewKind != "" {
		req["kind"] = agentNewKind
	}
	if len(args) == 2 {
		req["prompt"] = args[1]
	}
	out, err := callAgentTool("agent.session.create", req)
	if err != nil {
		return err
	}
	fmt.Println(out["session"])
	return nil
}

func runAgentSessionSend(_ *cobra.Command, args []string) error {
	out, err := callAgentTool("agent.prompt.send", map[string]any{
		"session": args[0],
		"text":    strings.Join(args[1:], " "),
		"origin":  agentSendFrom,
	})
	if err != nil {
		return err
	}
	fmt.Println(out["prompt_id"])
	return nil
}

func runAgentSessionCancel(_ *cobra.Command, args []string) error {
	if len(args) == 2 {
		_, err := callAgentTool("agent.prompt.cancel", map[string]any{
			"session": args[0], "prompt_id": args[1],
		})
		return err
	}
	_, err := callAgentTool("agent.turn.interrupt", map[string]any{"session": args[0]})
	return err
}

func runAgentSessionTail(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	session := ""
	if len(args) == 1 {
		session = args[0]
	}
	cursor := agentTailAfter
	for {
		out, err := callAgentTool("agent.events.read", map[string]any{
			"session": session, "after": cursor, "limit": 200,
		})
		if err != nil {
			return err
		}
		events, _ := out["events"].([]any)
		for _, e := range events {
			printAgentEvent(e)
		}
		if next, ok := out["next"].(float64); ok {
			cursor = int64(next)
		}
		if len(events) >= 200 {
			continue
		}
		if !agentTailFlw {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(agentTailPollInterval):
		}
	}
}

func printAgentEvent(e any) {
	m, _ := e.(map[string]any)
	payload, _ := json.Marshal(m["payload"])
	ts, _ := m["ts"].(string)
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		ts = t.Local().Format("15:04:05")
	}
	fmt.Printf("%v %s %-20v %-22v %s\n", m["host_seq"], ts, m["session"], m["type"], payload)
}
