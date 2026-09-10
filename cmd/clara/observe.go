package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/loghub"
	"github.com/brightpuddle/clara/internal/ringbuf"
)

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

// streamLogs connects to the daemon control socket and prints streaming log entries.
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
