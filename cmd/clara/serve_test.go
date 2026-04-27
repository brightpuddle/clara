package main

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestRunDaemonServices_StartsServersBeforeSupervisor(t *testing.T) {
	startedServers := false
	supervisorSawStartedServers := false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runDaemonServices(ctx, daemonServiceHooks{
			startControl: func(ctx context.Context) error {
				startedServers = true
				<-ctx.Done()
				return nil
			},
			startSupervisor: func(ctx context.Context) error {
				// Give control server a moment to "start"
				time.Sleep(50 * time.Millisecond)
				supervisorSawStartedServers = startedServers
				cancel()
				<-ctx.Done()
				return nil
			},
		}, zerolog.Nop())
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runDaemonServices returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for daemon services to exit")
	}

	if !supervisorSawStartedServers {
		t.Fatal("expected MCP servers to start before supervisor")
	}
}
