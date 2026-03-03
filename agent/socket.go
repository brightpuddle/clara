package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/brightpuddle/clara/internal/xdg"
)

// AgentSocketName is the filename of the agent's Unix domain socket.
const AgentSocketName = "agent.sock"

// AgentSocketPath returns the full path to the agent Unix socket.
func AgentSocketPath() (string, error) {
	return xdg.RuntimeFile(AgentSocketName)
}

// agentStats holds live counters updated by the main agent loop.
type agentStats struct {
	startTime      time.Time
	actionsApplied atomic.Int64
	filesIngested  atomic.Int64
	notesDir       string
	serverAddr     string
	pid            int
}

func newAgentStats(notesDir, serverAddr string) *agentStats {
	return &agentStats{
		startTime:  time.Now(),
		notesDir:   notesDir,
		serverAddr: serverAddr,
		pid:        os.Getpid(),
	}
}

// statusResponse is the JSON payload returned for a "status" command.
type statusResponse struct {
	Running        bool   `json:"running"`
	PID            int    `json:"pid"`
	Uptime         string `json:"uptime"`
	NotesDir       string `json:"notes_dir"`
	ServerAddr     string `json:"server_addr"`
	ActionsApplied int64  `json:"actions_applied"`
	FilesIngested  int64  `json:"files_ingested"`
}

// serveSocket opens a Unix domain socket and handles control connections until
// ctx is cancelled.  cancel is called when a "stop" command is received.
func serveSocket(ctx context.Context, stats *agentStats, cancel context.CancelFunc) {
	path, err := AgentSocketPath()
	if err != nil {
		slog.Warn("socket: could not resolve path", "err", err)
		return
	}

	// Remove stale socket from a previous run.
	_ = os.Remove(path)

	ln, err := net.Listen("unix", path)
	if err != nil {
		slog.Warn("socket: listen failed", "path", path, "err", err)
		return
	}
	defer func() {
		ln.Close()
		os.Remove(path)
	}()

	slog.Info("agent socket ready", "path", path)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return // context cancelled
			}
			slog.Warn("socket: accept error", "err", err)
			continue
		}
		go handleSocketConn(conn, stats, cancel)
	}
}

func handleSocketConn(conn net.Conn, stats *agentStats, cancel context.CancelFunc) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	var req struct {
		Cmd string `json:"cmd"`
	}
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		return
	}

	var resp any
	switch req.Cmd {
	case "status":
		uptime := time.Since(stats.startTime).Round(time.Second)
		resp = statusResponse{
			Running:        true,
			PID:            stats.pid,
			Uptime:         uptime.String(),
			NotesDir:       stats.notesDir,
			ServerAddr:     stats.serverAddr,
			ActionsApplied: stats.actionsApplied.Load(),
			FilesIngested:  stats.filesIngested.Load(),
		}
	case "stop":
		resp = map[string]bool{"ok": true}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(conn, string(data))
		cancel()
		return
	default:
		resp = map[string]string{"error": fmt.Sprintf("unknown command %q", req.Cmd)}
	}

	data, _ := json.Marshal(resp)
	fmt.Fprintln(conn, string(data))
}
