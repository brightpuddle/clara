package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/rs/zerolog"
)

func TestBuildHandler_ShutdownCancelsDaemon(t *testing.T) {
	shutdownCh := make(chan struct{}, 1)
	handler := buildHandler(nil, nil, nil, nil, zerolog.Nop(), func() {
		shutdownCh <- struct{}{}
	})

	w := &testResponseWriter{}
	handler(context.Background(), &ipc.Request{Method: ipc.MethodShutdown}, w)
	if w.resp == nil {
		t.Fatal("expected shutdown response")
	}
	if w.resp.Message != "shutdown initiated" {
		t.Fatalf("unexpected shutdown message: %q", w.resp.Message)
	}

	select {
	case <-shutdownCh:
	case <-time.After(time.Second):
		t.Fatal("expected shutdown callback to be invoked")
	}
}

type testResponseWriter struct {
	resp *ipc.Response
}

func (w *testResponseWriter) Write(resp *ipc.Response) error {
	w.resp = resp
	return nil
}

func TestTailFileLinesReturnsLastNLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clara.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatalf("write log file: %v", err)
	}

	lines, err := tailFileLines(path, 2)
	if err != nil {
		t.Fatalf("tailFileLines returned error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "three" || lines[1] != "four" {
		t.Fatalf("unexpected tailed lines: %#v", lines)
	}
}
